package extract

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Suppression is the negative half of the product, and the half that is easy to
// get wrong: proving recall is easy, proving restraint is not. Everything the
// profile bans is detected here, recorded with the user's own bullet and its
// line number, and emitted as a `suppressions` signal so the audit trail shows
// what was held back and why — while every other extractor consults the same
// set and stays quiet about it.
//
// Two design decisions worth defending:
//
//   - **Suppression is scoped, not global.** An email suppressed as a newsletter
//     is invisible to every extractor. A calendar invite suppressed as "already
//     accepted" is only suppressed from being *listed*: it still participates in
//     conflict detection, because an accepted meeting colliding with a
//     pediatrician appointment is exactly the collision the digest exists to
//     catch. Suppressing the item everywhere would let a profile preference
//     delete a fact.
//   - **The profile can contradict itself, and we resolve it in the open.**
//     "Long threads where I've already had the last word" must not surface;
//     "quiet investor threads … after three business days" must. Both are the
//     user's words. The last-word rule therefore stops at P0/P1: the detail line
//     on the resulting signal cites both bullets so the resolution is visible
//     rather than buried in a precedence table.
//
// A bullet this parser cannot classify is left inert and listed by
// UnhandledSuppressions rather than approximated. Guessing at a suppression the
// user wrote and getting it wrong is worse than admitting we did not understand
// it.

// The rule slugs this parser understands. They are internal identifiers; the
// text a reader sees is always the user's own bullet.
const (
	ruleNewsletter     = "newsletters"
	ruleMarketing      = "marketing"
	ruleAcceptedInvite = "accepted-invites"
	ruleLastWord       = "last-word"
	ruleFYIOnly        = "fyi-only"
	ruleRecruiters     = "recruiters"
)

// suppression records that one item was held back, and by which bullet.
type suppression struct {
	RuleID string
	Rule   model.Rule
	Why    string
}

// suppressedItem is one held-back record, in emission order.
type suppressedItem struct {
	Source model.Source
	Ref    string
	Title  string
	Supp   suppression
}

// recruiterPattern is the carve-out in the recruiter rule: individually
// suppressed, collectively worth a line ("three+ from the same firm in a week,
// then surface as a pattern").
type recruiterPattern struct {
	Domain string
	Refs   []string
	Rule   model.Rule
}

// suppressor holds the computed suppression sets for one corpus.
type suppressor struct {
	profilePath string

	emails  map[string]suppression
	events  map[string]suppression
	threads map[string]suppression

	// byRule keeps the profile bullet behind each classified rule, so an
	// extractor that overrides one can quote it.
	byRule map[string]model.Rule

	items     []suppressedItem
	patterns  []recruiterPattern
	unhandled []model.Rule
}

func (s *suppressor) email(id string) (suppression, bool)  { v, ok := s.emails[id]; return v, ok }
func (s *suppressor) thread(id string) (suppression, bool) { v, ok := s.threads[id]; return v, ok }

// UnhandledSuppressions lists the profile's suppression bullets this parser
// could not turn into a rule. They are inert: nothing is suppressed on their
// account. Surfacing them is the honest alternative to pretending the list was
// fully understood.
func (t *Toolbox) UnhandledSuppressions() []model.Rule { return t.supp.unhandled }

// ProfilePath is where the profile that drove these decisions was read from,
// for a caller rendering its own prose. With no profile it is the conventional
// filename, so a reference never renders as a bare colon.
func (t *Toolbox) ProfilePath() string { return t.profileRef() }

// newSuppressor computes every suppression set, in a fixed order.
func newSuppressor(t *Toolbox) *suppressor {
	s := &suppressor{
		emails:  map[string]suppression{},
		events:  map[string]suppression{},
		threads: map[string]suppression{},
		byRule:  map[string]model.Rule{},
	}
	p := t.corpus.Profile
	if p == nil {
		return s
	}
	s.profilePath = p.Path

	for _, bullet := range p.Suppressions {
		id := classifySuppressionRule(bullet.Text)
		if _, dup := s.byRule[id]; id != "" && !dup {
			s.byRule[id] = bullet
		}
		switch id {
		case ruleNewsletter:
			s.applyBulk(t, bullet, ruleNewsletter, "newsletter")
		case ruleMarketing:
			s.applyBulk(t, bullet, ruleMarketing, "marketing")
		case ruleAcceptedInvite:
			s.applyAcceptedInvites(t, bullet)
		case ruleLastWord:
			s.applyLastWord(t, bullet)
		case ruleFYIOnly:
			s.applyFYIOnly(t, bullet)
		default:
			s.unhandled = append(s.unhandled, bullet)
		}
	}

	s.applyRecruiters(t)
	return s
}

// classifySuppressionRule maps a profile bullet onto one of the rules this
// parser can apply, by the distinguishing word the user actually wrote.
func classifySuppressionRule(text string) string {
	l := strings.ToLower(text)
	switch {
	case containsWord(l, "newsletter") || containsWord(l, "newsletters"):
		return ruleNewsletter
	case containsWord(l, "marketing") || containsWord(l, "promotional"):
		return ruleMarketing
	case (containsWord(l, "invite") || containsWord(l, "invites") || containsWord(l, "invitation")) &&
		(containsWord(l, "accepted") || containsWord(l, "already accepted")):
		return ruleAcceptedInvite
	case containsWord(l, "last word"):
		return ruleLastWord
	case containsWord(l, "fyi"):
		return ruleFYIOnly
	}
	return ""
}

// record marks one item suppressed, keeping the first rule that claimed it —
// an item banned twice is still banned once, and the first bullet in the user's
// own document order is the one that explains it.
func (s *suppressor) record(source model.Source, ref, title string, sup suppression) {
	var set map[string]suppression
	switch source {
	case model.SourceEmail:
		set = s.emails
	case model.SourceCalendar:
		set = s.events
	default:
		return
	}
	if _, dup := set[ref]; dup {
		return
	}
	set[ref] = sup
	s.items = append(s.items, suppressedItem{Source: source, Ref: ref, Title: title, Supp: sup})
}

// applyBulk suppresses bulk mail — the newsletter and marketing rules, which
// differ only in the label they look for.
//
// Three signals identify bulk mail: the message label, a mailbox that only ever
// sends bulk ("noreply@", "newsletter@"), and any proper noun in the user's own
// bullet. That last one is why "Even Stratechery." suppresses
// newsletter@stratechery.example: the rule matches on the name the user
// bothered to write down.
func (s *suppressor) applyBulk(t *Toolbox, bullet model.Rule, ruleID, label string) {
	named := properNounTokens(bullet.Text)

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		why := ""
		switch {
		case hasLabel(e.Labels, label):
			why = "labelled " + label
		case isMachineSender(e.From, e.Labels):
			why = "bulk sender " + e.From.Email
		default:
			sender := stemAll(e.From.Name, domainOf(e.From.Email), localOf(e.From.Email))
			for _, tok := range named {
				if slices.Contains(sender, tok) {
					why = "sender named in the rule (" + tok + ")"
					break
				}
			}
		}
		if why == "" {
			continue
		}
		s.record(model.SourceEmail, e.ID, e.Subject, suppression{RuleID: ruleID, Rule: bullet, Why: why})
	}
}

// applyAcceptedInvites suppresses calendar events the owner already accepted —
// from being listed, not from being reasoned about (see the package comment).
func (s *suppressor) applyAcceptedInvites(t *Toolbox, bullet model.Rule) {
	for i := range t.corpus.Events {
		ev := &t.corpus.Events[i]
		if ev.PartStatOf(t.ownerAddr) != model.PartStatAccepted {
			continue
		}
		s.record(model.SourceCalendar, ev.UID, ev.Summary, suppression{
			RuleID: ruleAcceptedInvite,
			Rule:   bullet,
			Why:    "already accepted",
		})
	}
}

// lastWordMinMessages is what "long thread" means here. Two messages is a
// question and an answer; the rule is about threads that ran on and then
// stopped with the user's own reply.
const lastWordMinMessages = 3

// applyLastWord suppresses long threads the owner closed and nobody reopened,
// except where the profile's own "what I might miss" section overrides it for
// the people who matter most.
func (s *suppressor) applyLastWord(t *Toolbox, bullet model.Rule) {
	for _, th := range t.threads {
		if len(th.Messages) < lastWordMinMessages || !th.OwnerHadLastWord || th.AwaitingReply {
			continue
		}
		if t.threadPriority(th).Rank() <= model.P1.Rank() {
			// The carve-out. Quiet-threads decides what to do with it.
			continue
		}
		s.threads[th.ID] = suppression{
			RuleID: ruleLastWord,
			Rule:   bullet,
			Why:    "owner had the last word and nobody replied",
		}
		last := th.Last()
		s.record(model.SourceEmail, last.ID, th.Subject, suppression{
			RuleID: ruleLastWord,
			Rule:   bullet,
			Why:    "owner had the last word and nobody replied",
		})
	}
}

// applyFYIOnly suppresses messages whose only action is "FYI".
func (s *suppressor) applyFYIOnly(t *Toolbox, bullet model.Rule) {
	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		if normAddr(e.From.Email) == t.ownerAddr {
			continue
		}
		text := e.Subject + "\n" + e.Body
		if !containsWord(text, "fyi") || asksSomething(text) {
			continue
		}
		s.record(model.SourceEmail, e.ID, e.Subject, suppression{
			RuleID: ruleFYIOnly,
			Rule:   bullet,
			Why:    "FYI with nothing asked",
		})
	}
}

var countPlusRE = regexp.MustCompile(`(\d+)\s*\+`)

// wordCounts is a slice rather than a map so the scan order is fixed: a map
// range here would let "two or three" resolve differently between runs.
var wordCounts = []struct {
	word string
	n    int
}{
	{"two", 2}, {"three", 3}, {"four", 4}, {"five", 5}, {"six", 6},
}

// defaultRecruiterPattern is the fallback threshold when the profile bullet
// names none.
const (
	defaultPatternCount  = 3
	defaultPatternWindow = 7 * 24 * time.Hour
)

// applyRecruiters implements the rule that lives in "People who matter" rather
// than the suppression list: a P4 cohort the user asked not to see, *unless*
// enough of them arrive from one firm in a week, at which point the pattern is
// the story.
func (s *suppressor) applyRecruiters(t *Toolbox) {
	p := t.corpus.Profile
	var entry *model.ProfilePerson
	for i := range p.People {
		note := strings.ToLower(p.People[i].Note)
		if p.People[i].Priority == model.P4 && (containsWord(note, "don't surface") ||
			containsWord(note, "dont surface") || containsWord(note, "do not surface")) {
			entry = &p.People[i]
			break
		}
	}
	if entry == nil {
		return
	}
	bullet := model.Rule{Text: entry.Name + " — " + entry.Note, Line: entry.Line}

	threshold, window := patternThreshold(entry.Note)
	byDomain := map[string][]string{}
	var domains []string

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		m := t.prio.Of(e.From, e.Labels)
		if m.Person == nil || m.Person.Line != entry.Line {
			continue
		}
		s.record(model.SourceEmail, e.ID, e.Subject, suppression{
			RuleID: ruleRecruiters,
			Rule:   bullet,
			Why:    "recruiter cold email",
		})
		if t.now.Sub(e.TS) <= window && !e.TS.After(t.now) {
			d := domainOf(e.From.Email)
			if _, seen := byDomain[d]; !seen {
				domains = append(domains, d)
			}
			byDomain[d] = append(byDomain[d], e.ID)
		}
	}

	for _, d := range domains {
		if len(byDomain[d]) >= threshold {
			s.patterns = append(s.patterns, recruiterPattern{Domain: d, Refs: byDomain[d], Rule: bullet})
		}
	}
}

// patternThreshold reads "three+ from the same firm in a week" out of the
// profile bullet, falling back to three-in-seven-days.
func patternThreshold(note string) (int, time.Duration) {
	count := defaultPatternCount
	if m := countPlusRE.FindStringSubmatch(note); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			count = n
		}
	} else {
		for _, wc := range wordCounts {
			if containsWord(note, wc.word+"+") || containsWord(note, wc.word) {
				count = wc.n
				break
			}
		}
	}

	window := defaultPatternWindow
	switch {
	case containsWord(note, "month"):
		window = 30 * 24 * time.Hour
	case containsWord(note, "week"):
		window = defaultPatternWindow
	case containsWord(note, "day"):
		window = 24 * time.Hour
	}
	return count, window
}

// properNounTokens are the capitalised words that are not sentence-initial —
// the names a user typed on purpose ("Even Stratechery."). Sentence-initial
// words are excluded because every sentence starts with a capital and matching
// on those would turn "Newsletters." into a token that matches nothing useful
// and "Marketing" into one that matches half the corpus.
func properNounTokens(text string) []string {
	var out []string
	for _, s := range sentences(text) {
		fields := strings.Fields(s)
		for i, f := range fields {
			if i == 0 {
				continue
			}
			trimmed := strings.Trim(f, `.,;:!?"'()`)
			if trimmed == "" {
				continue
			}
			r := []rune(trimmed)[0]
			if r < 'A' || r > 'Z' {
				continue
			}
			for _, w := range words(trimmed) {
				if len(w) >= 4 && !stopwords[w] {
					if st := stem(w); !slices.Contains(out, st) {
						out = append(out, st)
					}
				}
			}
		}
	}
	return out
}

func hasLabel(labels []string, want string) bool {
	w := stem(strings.ToLower(want))
	for _, l := range labels {
		if stem(strings.ToLower(strings.TrimSpace(l))) == w {
			return true
		}
	}
	return false
}

// threadPriority is the most urgent priority among a thread's counterparts.
func (t *Toolbox) threadPriority(th *Thread) model.Priority {
	best := defaultPriority
	for _, m := range th.Messages {
		if normAddr(m.From.Email) == t.ownerAddr {
			continue
		}
		best = atMost(best, t.prio.Of(m.From, m.Labels).Priority)
	}
	for _, c := range th.Counterparts {
		best = atMost(best, t.prio.Of(c, nil).Priority)
	}
	return best
}

// Suppressions reports what the profile held back, and the one thing it asked
// to be shown instead.
//
// These signals are the audit trail, not digest content: they carry
// section_hint "honesty" so the page can say "14 items held back per your
// profile" rather than listing them. The eval reads them as the proof that a
// negative trap was seen and deliberately dropped, which is a stronger claim
// than "it never appeared".
func (t *Toolbox) Suppressions() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	for _, item := range t.supp.items {
		title := collapse(item.Title)
		if title == "" {
			title = item.Ref
		}
		out = append(out, model.Signal{
			ID:          g.next(model.KindSuppressions, string(item.Source), item.Ref),
			Kind:        model.KindSuppressions,
			Priority:    model.P4,
			Title:       "held back: " + truncate(title, 80),
			Detail:      fmt.Sprintf("%s — %s (%s)", item.Supp.Why, quote(item.Supp.Rule.Text), t.ruleRef(item.Supp.Rule)),
			Citations:   []model.Citation{{Source: item.Source, Ref: item.Ref}},
			SectionHint: model.SectionHonesty,
			Confidence:  model.Certain,
		})
	}

	for _, p := range t.supp.patterns {
		cites := make([]model.Citation, 0, len(p.Refs))
		for _, ref := range p.Refs {
			cites = append(cites, emailCite(ref))
		}
		out = append(out, model.Signal{
			ID:       g.next(model.KindSuppressions, "pattern", p.Domain),
			Kind:     model.KindSuppressions,
			Priority: model.P3,
			Title:    fmt.Sprintf("%d cold emails from %s this week", len(p.Refs), p.Domain),
			Detail: fmt.Sprintf("individually suppressed, surfaced as a pattern — %s (%s)",
				quote(p.Rule.Text), t.ruleRef(p.Rule)),
			Citations:   cites,
			SectionHint: model.SectionPulse,
			Confidence:  model.Certain,
		})
	}

	return out, nil
}

// ruleRef renders "profile.md:32" for a detail line.
func (t *Toolbox) ruleRef(r model.Rule) string {
	return t.profileRef() + ":" + strconv.Itoa(r.Line)
}
