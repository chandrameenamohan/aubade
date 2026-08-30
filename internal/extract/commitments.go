package extract

import (
	"fmt"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The commitment tracker is the extractor the HLD calls genuinely hard (§11):
// "about to drop" is a statement about what did *not* happen, so there is no
// record to find — only a promise, and then an absence where the delivery
// should be.
//
// The shape of the answer is three questions, each of which has to be answerable
// from the corpus alone:
//
//  1. Did the owner promise something? A first-person future ("I'll send…"), or
//     a reply that is nothing but a deadline ("tonight.") to a message that
//     asked for something — which is how people actually answer a direct ask.
//  2. By when? A deadline phrase in the same sentence, resolved against the
//     moment the promise was written. Sentence-scoped on purpose: "I'll send the
//     deck. Separately, the board meeting is Thursday" is a promise with no date
//     and a date with no promise, and reading the body as one blob fuses them
//     into a deadline nobody agreed to.
//  3. Did it get delivered? A later message from the owner in the same thread
//     that hands something over, a counterparty acknowledging receipt, or a
//     checked-off task that matches. Any of those and the commitment is closed —
//     silently. The kept promises are the ones a digest must never mention.
//
// Suppression does not apply here. The profile's suppression list is about
// inbound noise ("newsletters", "FYI"); nothing in it was written to hide what
// the user themselves promised, and a thread the user closed with the last word
// is still a thread they may owe something in.

// promisePhrases mark a sentence in which the owner takes something on. Matched
// per sentence, whole-word, apostrophe-insensitive.
var promisePhrases = []string{
	"i'll", "we'll", "i will", "we will",
	"i'm going to", "we're going to", "i am going to", "we are going to",
	"i can get", "i can send", "i can have", "i can share", "i can put",
	"let me send", "let me get", "let me pull", "let me put", "let me know by",
	"you'll have", "you will have",
	// Present participles read as promises only because this list is matched
	// against mail the *owner sent*: "sending the deck friday" is a commitment
	// in an outbox and a description in an inbox.
	"sending the", "sending you", "sending it", "sending over", "i'm sending", "we're sending",
	"will send", "will get you", "will have", "will share", "will circulate",
	"will follow up", "will push", "will ship", "will get back",
	"i promise", "i owe you", "we owe you", "consider it done", "on it",
}

// deliveryPhrases mark the message that closes a promise. They are perfective
// on purpose — "sent", not "send" — so the promise itself can never read as its
// own delivery.
var deliveryPhrases = []string{
	"attached", "attaching", "here's the", "here is the", "here you go",
	"just sent", "sent you", "sent it", "sent the", "sent over", "have sent",
	"shared the", "sharing the", "uploaded", "in your inbox", "went out",
	"signed and sent", "it's done", "that's done", "now done", "all set",
	"is live", "shipped it", "pushed it", "link below", "see below",
}

// ackPhrases are a counterparty confirming they got it.
var ackPhrases = []string{
	"got it", "received", "thanks for sending", "thanks for the",
	"have it", "this is great", "perfect, thanks", "looks good",
}

// noteCommitPhrases mark a promise recorded in a meeting note rather than in
// mail — the "promises I made and didn't put on a todo list" case the profile
// asks for help with.
var noteCommitPhrases = []string{
	"committed to", "commitment to", "i owe", "we owe", "promised",
	"action item", "action:", "i'll", "we'll", "agreed to send",
}

// overduePhrases are how a note says a commitment has already slipped.
var overduePhrases = []string{"overdue", "still owe", "outstanding", "slipped", "past due", "late"}

// deliveryTokenOverlap is how many distinctive words a candidate delivery has to
// share with the promise before it counts as the same thing. Two is enough to
// tie "the updated cap table" to "cap table attached" and not enough to tie it
// to an unrelated thread.
const deliveryTokenOverlap = 2

// Commitments reports promises the owner made and has not visibly kept.
func (t *Toolbox) Commitments() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		if normAddr(e.From.Email) != t.ownerAddr || e.TS.After(t.now) {
			continue
		}
		out = append(out, t.commitmentsInEmail(g, e)...)
	}
	for i := range t.corpus.Notes {
		out = append(out, t.commitmentsInNote(g, &t.corpus.Notes[i])...)
	}
	return out, nil
}

// commitmentsInEmail finds every unkept promise in one message the owner sent.
func (t *Toolbox) commitmentsInEmail(g *ids, e *model.Email) model.Signals {
	th := t.threadByID[e.ThreadID]
	if th == nil {
		return nil
	}
	trigger, hasTrigger := t.triggerFor(th, e)

	var out model.Signals
	for _, sentence := range sentences(e.Body) {
		refs := ParseDueRefs(sentence, e.TS, t.loc)
		if len(refs) == 0 {
			continue
		}

		phrase, promised := firstMatch(sentence, promisePhrases)
		bare := false
		if !promised {
			if !hasTrigger || !isBareDeadline(sentence, refs) {
				continue
			}
			bare = true
		}

		due := refs[0]
		// A kept promise is deliberately silent: the digest exists to name the
		// ones that are about to be dropped.
		if t.delivered(th, e, sentence) {
			continue
		}

		owed := t.owedTo(e)
		match := t.prio.Of(owed, e.Labels)
		priority, section := commitmentUrgency(match.Priority, due.Deadline, t.now, t.day, t.loc)

		cites := []model.Citation{emailCite(e.ID)}
		if hasTrigger {
			cites = append(cites, emailCite(trigger.ID))
		}
		detail := fmt.Sprintf("%s in %s — %s. No delivering follow-up in the thread.",
			commitmentSource(bare, sentence, phrase), quote(th.Subject), t.deadlinePhrase(due.Deadline))
		if task, ok := t.openTaskFor(sentence + " " + e.Subject); ok {
			cites = append(cites, taskCite(task.ID))
			detail += fmt.Sprintf(" Still open on the list as %s.", quote(task.Title))
		}
		detail += " Owed to " + owed.String() + ", " + match.Cited(t.profileRef()) + "."

		out = append(out, model.Signal{
			ID:          g.next(model.KindCommitments, e.ID),
			Kind:        model.KindCommitments,
			Priority:    priority,
			Title:       commitmentTitle(owed, th.Subject, due, t.now),
			Detail:      detail,
			Citations:   dedupeCitations(cites),
			SectionHint: section,
			Confidence:  model.Certain,
			Deadline:    timePtr(due.Deadline),
		})
	}
	return out
}

// commitmentSource names how the promise was recognised, so a reader can tell
// an explicit "I'll send it" from the inference drawn off a bare deadline — and
// can see which words fired, which is what makes a false positive arguable
// rather than mysterious.
func commitmentSource(bare bool, sentence, phrase string) string {
	if bare {
		return "Answered a direct ask with nothing but a deadline: " + quote(sentence)
	}
	return fmt.Sprintf("Promised %s (on %q)", quote(sentence), phrase)
}

// triggerFor is the counterparty message the owner was replying to — the ask
// that a bare deadline answers, and a second citation for the signal.
func (t *Toolbox) triggerFor(th *Thread, e *model.Email) (model.Email, bool) {
	if e.InReplyTo != "" {
		if prev, ok := t.emails[e.InReplyTo]; ok && asksSomething(prev.Body+" "+prev.Subject) {
			return *prev, true
		}
		return model.Email{}, false
	}
	for i := len(th.Messages) - 1; i >= 0; i-- {
		m := th.Messages[i]
		if m.ID == e.ID {
			continue
		}
		if m.TS.After(e.TS) || normAddr(m.From.Email) == t.ownerAddr {
			continue
		}
		if asksSomething(m.Body + " " + m.Subject) {
			return m, true
		}
	}
	return model.Email{}, false
}

// isBareDeadline reports whether a sentence is nothing but a deadline — the
// "tonight." reply. Everything the deadline phrases matched is removed; one
// leftover word ("by", "eod") is allowed, more than one is prose.
func isBareDeadline(sentence string, refs []DueRef) bool {
	rest := strings.ToLower(sentence)
	for _, r := range refs {
		rest = strings.ReplaceAll(rest, strings.ToLower(r.Text), " ")
	}
	return len(words(rest)) <= 1
}

// delivered reports whether a promise was visibly kept: a later message from
// the owner in the same thread that hands something over, a counterparty
// acknowledging receipt, or a checked-off task that names the same thing.
func (t *Toolbox) delivered(th *Thread, promise *model.Email, sentence string) bool {
	for _, m := range th.After(*promise) {
		if m.TS.After(t.now) {
			continue
		}
		if normAddr(m.From.Email) == t.ownerAddr {
			if containsAny(m.Body, deliveryPhrases) {
				return true
			}
			continue
		}
		if containsAny(m.Body, ackPhrases) {
			return true
		}
	}
	return t.closedByTask(sentence + " " + promise.Subject)
}

// closedByTask reports whether a checked-off task covers the same ground.
func (t *Toolbox) closedByTask(text string) bool {
	want := distinctiveTokens(text)
	for i := range t.corpus.Tasks {
		task := &t.corpus.Tasks[i]
		if task.Done && overlapCount(distinctiveTokens(task.Title), want) >= deliveryTokenOverlap {
			return true
		}
	}
	return false
}

// openTaskFor finds an unfinished task that plainly tracks the same promise, so
// the signal can cite the list entry alongside the mail.
func (t *Toolbox) openTaskFor(text string) (*model.Task, bool) {
	want := distinctiveTokens(text)
	for i := range t.corpus.Tasks {
		task := &t.corpus.Tasks[i]
		if task.Done {
			continue
		}
		if overlapCount(distinctiveTokens(task.Title), want) >= deliveryTokenOverlap {
			return task, true
		}
	}
	return nil, false
}

// owedTo is who the promise was made to: the first recipient who is not the
// owner, falling back to the first recipient at all.
func (t *Toolbox) owedTo(e *model.Email) model.Person {
	for _, p := range e.To {
		if normAddr(p.Email) != t.ownerAddr {
			return p
		}
	}
	for _, p := range e.CC {
		if normAddr(p.Email) != t.ownerAddr {
			return p
		}
	}
	if len(e.To) > 0 {
		return e.To[0]
	}
	return model.Person{}
}

// commitmentUrgency turns a base priority and a deadline into the priority and
// section the digest reads.
//
// An overdue promise is never quieter than P1 — a slipped promise to a P2
// contact is still a slipped promise — and an overdue P0 promise is the one
// thing to do right now, which is the section the sample digest opens with.
func commitmentUrgency(base model.Priority, deadline, now, day time.Time, loc *time.Location) (model.Priority, string) {
	switch {
	case deadline.Before(now):
		p := atMost(base, model.P1)
		if p == model.P0 {
			return p, model.SectionOneThingNow
		}
		return p, model.SectionUrgentToday
	case sameDay(deadline, day, loc):
		return atMost(base, model.P2), model.SectionUrgentToday
	default:
		return base, model.SectionPulse
	}
}

// commitmentTitle is the one line a reader sees.
func commitmentTitle(owed model.Person, subject string, due DueRef, now time.Time) string {
	who := owed.Name
	if who == "" {
		who = owed.Email
	}
	state := "due " + due.Text
	if due.Deadline.Before(now) {
		state = "overdue (" + due.Text + ")"
	}
	if who == "" {
		return fmt.Sprintf("unkept promise on %q — %s", truncate(subject, 60), state)
	}
	return fmt.Sprintf("unkept promise to %s on %q — %s", who, truncate(subject, 60), state)
}

// deadlinePhrase renders a deadline relative to the anchor morning.
func (t *Toolbox) deadlinePhrase(deadline time.Time) string {
	d := deadline.In(t.loc)
	switch {
	case d.Before(t.now):
		return fmt.Sprintf("deadline %s has passed", d.Format("Mon 2 Jan 15:04"))
	case sameDay(d, t.day, t.loc):
		return fmt.Sprintf("due today at %s", d.Format("15:04"))
	default:
		return fmt.Sprintf("due %s", d.Format("Mon 2 Jan 15:04"))
	}
}

// profileRef is the profile path detail lines name, falling back to the
// conventional filename when there is no profile to read one from.
func (t *Toolbox) profileRef() string {
	if p := t.supp.profilePath; p != "" {
		return p
	}
	return sourcePaths["profile"]
}

// commitmentsInNote finds promises recorded in a meeting note and never
// delivered. Notes are where the "promise I made and didn't put on a todo list"
// actually lives, and a note has no thread to check, so delivery is looked for
// across the owner's later mail and the task list instead.
func (t *Toolbox) commitmentsInNote(g *ids, n *model.Note) model.Signals {
	if !n.HasDate() || n.Date.After(t.now) {
		return nil
	}
	var out model.Signals

	for _, sentence := range sentences(n.Body) {
		if !containsAny(sentence, noteCommitPhrases) {
			continue
		}
		refs := ParseDueRefs(sentence, n.Date, t.loc)
		flagged := containsAny(n.Body, overduePhrases)
		if len(refs) == 0 && !flagged {
			continue
		}
		if t.noteCommitmentDelivered(n, sentence) {
			continue
		}

		owed, match := t.personInText(sentence)
		priority := match.Priority
		var deadline *time.Time
		section := model.SectionPulse
		confidence := model.Unsure
		if len(refs) > 0 {
			deadline = timePtr(refs[0].Deadline)
			priority, section = commitmentUrgency(match.Priority, refs[0].Deadline, t.now, t.day, t.loc)
			confidence = model.Certain
		} else {
			priority = atMost(priority, model.P2)
			section = model.SectionNotSure
		}

		title := fmt.Sprintf("promise recorded in %s and not delivered", n.Path)
		if owed != "" {
			title = fmt.Sprintf("promise to %s recorded in %s and not delivered", owed, n.Path)
		}
		detail := fmt.Sprintf("The note says %s. Nothing in the owner's later mail or the task list closes it.", quote(sentence))
		if flagged {
			detail += " The note itself calls it overdue, but carries no date to hold it to."
		}

		out = append(out, model.Signal{
			ID:          g.next(model.KindCommitments, "note", n.Path),
			Kind:        model.KindCommitments,
			Priority:    priority,
			Title:       title,
			Detail:      detail,
			Citations:   []model.Citation{noteCite(n.Path)},
			SectionHint: section,
			Confidence:  confidence,
			Deadline:    deadline,
		})
	}
	return out
}

// noteCommitmentDelivered looks for the delivery a note commitment would have
// produced: a later message from the owner that hands the thing over, or a
// checked-off task that names it.
func (t *Toolbox) noteCommitmentDelivered(n *model.Note, sentence string) bool {
	want := distinctiveTokens(sentence + " " + n.Title)

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		if normAddr(e.From.Email) != t.ownerAddr || !e.TS.After(n.Date) || e.TS.After(t.now) {
			continue
		}
		if !containsAny(e.Body, deliveryPhrases) {
			continue
		}
		if overlapCount(distinctiveTokens(e.Subject+" "+e.Body), want) >= deliveryTokenOverlap {
			return true
		}
	}
	return t.closedByTask(sentence + " " + n.Title)
}

// personInText finds the profile person a sentence names, by first name or full
// name. It is how "Committed to Diane: a written update every quarter" gets
// Diane Okafor's P1 rather than a default.
func (t *Toolbox) personInText(text string) (string, Match) {
	p := t.corpus.Profile
	if p == nil {
		return "", Match{Priority: defaultPriority}
	}
	for i := range p.People {
		person := &p.People[i]
		if !looksLikePersonName(person.Name) {
			continue
		}
		names := strings.Fields(person.Name)
		if containsWord(text, person.Name) || (len(names) > 0 && containsWord(text, names[0])) {
			return person.Name, Match{Priority: person.Priority, Person: person, Reason: person.Note}
		}
	}
	return "", Match{Priority: defaultPriority}
}
