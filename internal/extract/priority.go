package extract

import (
	"slices"
	"strconv"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The priority map is how profile.md becomes policy.
//
// "People who matter" is a list of entries with a P0..P4 beside each, and two
// different kinds of entry live in it:
//
//	- **Named people** — "**Marcus Webb** (Series A lead …) — P0 during the
//	  raise". Matched on the display name, or on a mailbox that spells the name
//	  (marcus@, m.webb@, marcus.webb@).
//	- **Cohorts** — "**The procurement leads at Halberd Manufacturing, Northstar
//	  Foods, and Veritas Components** … P1", "**Recruiters cold-emailing me** —
//	  P4". These name no mailbox at all, so they are matched on their
//	  distinctive words against the sender's domain, display name and labels:
//	  "Halberd" finds renee@halberd.example, "Recruiters" finds a message
//	  labelled recruiter.
//
// Matching is word-equality on stemmed tokens, never substring. Substring
// matching on a name list is how "Sam" starts matching "Samantha" and someone's
// morning gets re-ranked by an accident of spelling.
//
// Anyone the profile does not mention defaults to P2 — worth reading, not worth
// interrupting a morning for — except that machine mail (newsletters,
// marketing, no-reply senders) floors at P4 whatever the profile says, because
// the profile's suppression list says so in the next section down.

// defaultPriority is where an unmatched human correspondent lands.
const defaultPriority = model.P2

// machinePriority is where obvious bulk mail lands regardless of sender.
const machinePriority = model.P4

// machineLabels mark bulk mail in the corpus label vocabulary.
var machineLabels = []string{"newsletter", "marketing", "promo", "promotion", "bulk", "automated"}

// machineLocals are mailbox names that only ever send bulk mail.
var machineLocals = []string{"noreply", "no-reply", "donotreply", "do-not-reply", "newsletter", "notifications", "mailer", "updates"}

// priorityEntry is one profile bullet, pre-tokenized for matching.
type priorityEntry struct {
	person model.ProfilePerson
	named  bool     // the bullet names a person rather than a cohort
	tokens []string // stemmed identity tokens
}

// priorityMap resolves a correspondent to a priority and to the profile bullet
// that decided it.
type priorityMap struct {
	entries []priorityEntry
}

func newPriorityMap(p *model.Profile) *priorityMap {
	m := &priorityMap{}
	if p == nil {
		return m
	}
	for _, person := range p.People {
		e := priorityEntry{person: person, named: looksLikePersonName(person.Name)}
		if e.named {
			for _, w := range words(person.Name) {
				if len(w) >= 2 {
					e.tokens = append(e.tokens, stem(w))
				}
			}
		} else {
			e.tokens = distinctiveTokens(person.Name)
		}
		if len(e.tokens) == 0 {
			continue
		}
		m.entries = append(m.entries, e)
	}
	return m
}

// looksLikePersonName reports whether a bullet's bold heading names one person.
//
// The test is shape, not a name database: at most three words, every word
// capitalised, and none of the connectives a group description needs ("the",
// "at", "and", a comma). "Sam Park" passes; "The procurement leads at Halberd
// Manufacturing, Northstar Foods, and Veritas Components" does not, and neither
// does "Recruiters cold-emailing me".
func looksLikePersonName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || strings.ContainsAny(n, ",/&") {
		return false
	}
	fields := strings.Fields(n)
	if len(fields) == 0 || len(fields) > 3 {
		return false
	}
	for _, f := range fields {
		lower := strings.ToLower(f)
		if lower == "the" || lower == "at" || lower == "and" || lower == "my" || lower == "me" {
			return false
		}
		r := []rune(f)
		if !strings.ContainsAny(string(r[0]), "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return false
		}
	}
	return true
}

// Match is what the profile decided about one correspondent.
type Match struct {
	Priority model.Priority
	// Person is the profile bullet that matched, or nil for a default.
	Person *model.ProfilePerson
	// Reason is a short human phrase for a detail line ("profile.md:19").
	Reason string
}

// Cited renders the profile provenance for a detail line. The profile is
// settings, not data, so it never becomes a Citation — but a signal that
// re-ranked someone's morning should still say which of their own bullets did
// it.
func (m Match) Cited(profilePath string) string {
	if m.Person == nil {
		return string(m.Priority) + " (default: not named in " + profilePath + ")"
	}
	return string(m.Priority) + " per " + profilePath + ":" + strconv.Itoa(m.Person.Line)
}

// Of resolves a correspondent, given their mailbox and any labels on the
// message that carried them.
//
// The first matching profile bullet wins, in document order, so the user
// controls precedence by writing the list in the order they mean it.
func (m *priorityMap) Of(p model.Person, labels []string) Match {
	nameWords := stemAll(p.Name)
	local := localOf(p.Email)
	localWords := stemAll(local)
	domainWords := stemAll(domainOf(p.Email))
	labelWords := stemAll(labels...)

	for i := range m.entries {
		e := &m.entries[i]
		if e.named {
			if matchesNamedPerson(e, p, nameWords, local, localWords) {
				return Match{Priority: e.person.Priority, Person: &e.person, Reason: e.person.Note}
			}
			continue
		}
		haystack := concat(nameWords, localWords, domainWords, labelWords)
		for _, tok := range e.tokens {
			if slices.Contains(haystack, tok) {
				return Match{Priority: e.person.Priority, Person: &e.person, Reason: e.person.Note}
			}
		}
	}

	if isMachineSender(p, labels) {
		return Match{Priority: machinePriority}
	}
	return Match{Priority: defaultPriority}
}

// matchesNamedPerson decides whether a mailbox belongs to a named profile entry.
func matchesNamedPerson(e *priorityEntry, p model.Person, nameWords []string, local string, localWords []string) bool {
	if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(e.person.Name)) {
		return true
	}
	// Every word of the profile name present in the display name.
	if len(nameWords) > 0 && overlapCount(nameWords, e.tokens) == len(e.tokens) {
		return true
	}
	// The mailbox spells the name: marcus@, marcus.webb@, m.webb@, marcuswebb@.
	if len(e.tokens) >= 2 {
		first, last := e.tokens[0], e.tokens[len(e.tokens)-1]
		joined := strings.Join(e.tokens, "")
		switch local {
		case joined, first + "." + last, first + last, string(first[0]) + last, string(first[0]) + "." + last:
			return true
		}
		if len(localWords) >= 2 && slices.Contains(localWords, first) && slices.Contains(localWords, last) {
			return true
		}
	}
	if len(e.tokens) == 1 && local == e.tokens[0] {
		return true
	}
	return false
}

// isMachineSender reports whether a message is bulk mail, by label or by
// mailbox. It is the floor under the priority map: the profile's suppression
// section bans newsletters and marketing outright, so nothing that looks like
// one is allowed to inherit a human's priority.
func isMachineSender(p model.Person, labels []string) bool {
	for _, l := range labels {
		if slices.Contains(machineLabels, stem(strings.ToLower(strings.TrimSpace(l)))) {
			return true
		}
	}
	local := localOf(p.Email)
	for _, m := range machineLocals {
		if local == m || strings.HasPrefix(local, m+"+") || strings.HasPrefix(local, m+".") {
			return true
		}
	}
	return false
}

// atMost returns the more urgent of two priorities (P0 beats P2).
func atMost(a, b model.Priority) model.Priority {
	if b.Rank() >= 0 && (a.Rank() < 0 || b.Rank() < a.Rank()) {
		return b
	}
	return a
}

// stemAll splits every input into words and returns their stems, deduplicated
// in first-seen order.
func stemAll(in ...string) []string {
	var out []string
	for _, s := range in {
		for _, w := range words(s) {
			if st := stem(w); !slices.Contains(out, st) {
				out = append(out, st)
			}
		}
	}
	return out
}

func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}
