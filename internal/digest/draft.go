package digest

import (
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Drafting is the third of the digest's three questions — "what can I dispatch
// right now" — and the only place the page writes sentences somebody might
// actually send. That makes it the place where honesty is easiest to lose, so
// two rules bound it:
//
//   - **A draft states nothing the corpus does not.** It quotes the ask back,
//     in the sender's own words, and leaves the answer as a marked blank. The
//     corpus contains the question; it does not contain the answer, and a
//     template that guessed one would be putting fabricated certainty in the
//     user's outbox under the user's name. That blank is the honest shape of a
//     one-reply item: everything but the sentence only the user can write.
//   - **A person the profile protects is never drafted for.** "For Sam: don't
//     draft. Surface and let me write." — the item still appears, with the rule
//     that stopped the draft cited beside it. Surfacing without drafting is the
//     behaviour the user asked for; silently dropping the item would be a
//     different, worse reading of the same sentence.

// Draft is a reply ready for the user to finish and send.
type Draft struct {
	// To is who it answers.
	To model.Person

	// Subject is the reply subject, already prefixed.
	Subject string

	// Body is the drafted text, in the resolved voice.
	Body string

	// Rules are the voice rules that shaped it, for the line under the draft.
	Rules []VoiceRule

	// Skipped is set when no draft was written; Body is then empty and the item
	// renders SkipReason instead. There are two reasons, and both are the voice
	// working rather than failing: the profile protects this correspondent, or
	// the composed text broke the user's own tone rules.
	Skipped    bool
	SkipReason string

	// Polished marks the more formal register.
	Polished bool
}

// draftFor builds the reply for one dispatchable signal, or reports that there
// is nothing to draft.
//
// Only mail is draftable. A dispatchable that came off the task list has no
// correspondent and no thread — "Re-run the inference cost model" is a job, not
// a reply — and inventing a recipient for it would be inventing a fact.
func (c *composer) draftFor(s model.Signal) *Draft {
	e := c.draftTarget(s)
	if e == nil {
		return nil
	}

	d := &Draft{To: e.From, Subject: replySubject(e.Subject)}
	if ok, rule := c.voice.DraftsFor(e.From); !ok {
		d.Skipped = true
		d.SkipReason = "your profile says to surface this and let you write it"
		if rule.Path != "" {
			d.SkipReason = fmt.Sprintf("%s (%s)", quoted(rule.Text), rule.Ref())
		}
		return d
	}

	entry := c.profilePerson(e.From)
	d.Polished, _ = c.voice.IsPolished(e.From, entry)

	body, ours := c.draftBody(e, d.Polished)
	// The guard: a draft goes out under the user's name, so a template that
	// broke their own tone rules is not shown as if it were their voice.
	if broken := c.voice.Check(ours); len(broken) > 0 {
		d.Skipped = true
		d.SkipReason = "aubade's own template broke your tone rules (" + strings.Join(broken, "; ") + "), so it is not offering one"
		return d
	}

	d.Body = body
	d.Rules = c.voice.Overrides()
	return d
}

// draftTarget is the message a draft would answer: the newest email the signal
// cites, which for a dispatchable is the message that carries the open ask.
func (c *composer) draftTarget(s model.Signal) *model.Email {
	var newest *model.Email
	for _, cite := range s.Citations {
		if cite.Source != model.SourceEmail {
			continue
		}
		e, ok := c.idx.emails[cite.Ref]
		if !ok {
			continue
		}
		if newest == nil || e.TS.After(newest.TS) {
			newest = e
		}
	}
	if newest == nil {
		return nil
	}
	// A message the owner sent is not something the owner replies to.
	if strings.EqualFold(strings.TrimSpace(newest.From.Email), strings.TrimSpace(c.in.Owner.Email)) {
		return nil
	}
	return newest
}

// draftBody writes the reply, and returns it twice: the whole thing, and the
// part aubade actually authored.
//
// The second return is what the voice guard inspects. A draft quotes the ask in
// the counterparty's own words, and those words are theirs — a recruiter who
// opens with "circling back" has not made the reply break the user's rule
// against writing it.
//
// Three lines, in the order the base voice asks for: the first line does the
// work (it names what this is about), the middle line is the blank only the
// user can fill, and the sign-off is whatever the profile said it was. Polished
// keeps the greeting capitalised and always signs — "for investors during the
// raise, slightly more polished. Still short."
func (c *composer) draftBody(e *model.Email, polished bool) (body, ours string) {
	var full, mine strings.Builder
	write := func(s string) { full.WriteString(s); mine.WriteString(s) }

	if greeting := c.greeting(e.From, polished); greeting != "" {
		write(greeting + "\n\n")
	}

	ask := firstQuestion(e.Body)
	if ask == "" {
		ask = strings.TrimSpace(e.Subject)
	}
	if ask == "" {
		write("answering here.")
	} else {
		// The length rule lands here: everything else in the draft is fixed, so
		// the quoted ask is the only part that can push it over the budget.
		full.WriteString(fmt.Sprintf("on %s — answering here.", quoted(clipWords(ask, c.voice.MaxWords-draftOverheadWords))))
		mine.WriteString("on — answering here.")
	}

	write("\n\n" + AnswerPlaceholder)
	if sign := c.signoff(polished); sign != "" {
		write("\n\n" + sign)
	}
	return full.String(), mine.String()
}

// draftOverheadWords is how many words the fixed parts of a draft use — the
// greeting, the frame around the quoted ask, the placeholder and the sign-off.
// The quoted ask gets whatever the voice's length rule leaves over.
const draftOverheadWords = 10

// greeting opens the draft in the voice's register.
func (c *composer) greeting(p model.Person, polished bool) string {
	name := shortName(p)
	if name == "" {
		return ""
	}
	switch {
	case c.voice.Greeting == GreetingNone:
		return ""
	case c.voice.Greeting == GreetingLowercase && !polished:
		return strings.ToLower("hi " + name + ",")
	default:
		return "Hi " + capitalize(name) + ","
	}
}

// signoff closes it. A polished draft always signs, even when the profile
// allows "or nothing", because the alternative reads as curt to someone who is
// not on the inside.
func (c *composer) signoff(polished bool) string {
	if c.voice.Signoff != "" {
		return c.voice.Signoff
	}
	if polished {
		return firstWord(c.in.Owner.Name)
	}
	return ""
}

// profilePerson is the profile entry for a correspondent, or nil when the
// profile does not name them.
func (c *composer) profilePerson(p model.Person) *model.ProfilePerson {
	profile := c.in.Corpus.Profile
	if profile == nil {
		return nil
	}
	if entry, ok := profile.PersonByName(p.Name); ok {
		return entry
	}
	if entry, ok := profile.PersonByName(p.Email); ok {
		return entry
	}
	return nil
}

// replySubject prefixes a subject with Re:, once.
func replySubject(subject string) string {
	s := strings.TrimSpace(subject)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}

// firstWord is the first whitespace-delimited word of s.
func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}
