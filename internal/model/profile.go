package model

import "strings"

// Rule is one bullet from profile.md, kept verbatim with the line it came from
// so a suppression or a tone choice can be cited back to the user's own words
// ("profile.md:112") rather than paraphrased into an anonymous policy.
type Rule struct {
	Text string `json:"text"`
	Line int    `json:"line"`
}

// ProfileSection is one "## " section of profile.md: its heading, its bullets,
// and its raw body. Every section is kept, including the ones aubade does not
// interpret — the profile is the user's document, and a loader that discards
// what it does not understand quietly narrows what the digest can ever learn.
type ProfileSection struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Bullets []Rule `json:"bullets,omitempty"`
	Line    int    `json:"line"`
}

// ProfilePerson is one entry from "## People who matter".
//
// Priority is the default weight for anything from this person; Priorities
// holds every priority the entry mentioned. Both exist because entries are
// allowed to be conditional — "Ben Schaffer … P0 during the raise, P2
// otherwise" — and flattening that to one number would lose the condition
// while pretending to be precise. Note keeps the sentence that carried it, so
// the orchestrator can read the condition it cannot compute.
type ProfilePerson struct {
	Name        string     `json:"name"`
	Role        string     `json:"role,omitempty"`
	Priority    Priority   `json:"priority"`
	Priorities  []Priority `json:"priorities,omitempty"`
	Conditional bool       `json:"conditional,omitempty"`
	Note        string     `json:"note,omitempty"`
	Line        int        `json:"line"`
}

// Profile is parsed profile.md: who the user is, who matters and how much, what
// must never be surfaced, and how a drafted reply should sound.
//
// The typed fields are the four the engine binds to. Sections keeps everything,
// in document order, so nothing in the user's file is lost to our parser's
// vocabulary.
type Profile struct {
	Path         string           `json:"path"`
	Owner        Person           `json:"owner"`
	People       []ProfilePerson  `json:"people"`
	Suppressions []Rule           `json:"suppressions"`
	ToneRules    []Rule           `json:"tone_rules"`
	HonestyRules []Rule           `json:"honesty_rules,omitempty"`
	MissRules    []Rule           `json:"miss_rules,omitempty"`
	Sections     []ProfileSection `json:"sections,omitempty"`
}

// PersonByName looks up a profile entry for a name.
//
// Matching is deliberately narrow: case-insensitive, on the whole name, with
// containment allowed only in the direction that is safe — the profile's name
// appearing inside the queried string, so "Sam Park <sam@tessera.io>" finds
// "Sam Park" but "Sam" does not. Fuzzy identity resolution (aliases, group
// entries like "the procurement leads at Halberd, Northstar and Veritas") is
// the toolbox's job with the corpus in hand; a loader guessing at it would
// mis-prioritise someone's morning on a substring.
func (p *Profile) PersonByName(name string) (*ProfilePerson, bool) {
	if p == nil {
		return nil, false
	}
	q := strings.ToLower(strings.TrimSpace(name))
	if q == "" {
		return nil, false
	}
	for i := range p.People {
		if strings.EqualFold(p.People[i].Name, name) {
			return &p.People[i], true
		}
	}
	for i := range p.People {
		n := strings.ToLower(strings.TrimSpace(p.People[i].Name))
		if n != "" && strings.Contains(q, n) {
			return &p.People[i], true
		}
	}
	return nil, false
}

// SectionByTitle finds a section whose heading contains the given text,
// case-insensitively. Headings are prose written by the user, so an exact match
// is the wrong bar.
func (p *Profile) SectionByTitle(contains string) (*ProfileSection, bool) {
	if p == nil {
		return nil, false
	}
	want := strings.ToLower(strings.TrimSpace(contains))
	if want == "" {
		return nil, false
	}
	for i := range p.Sections {
		if strings.Contains(strings.ToLower(p.Sections[i].Title), want) {
			return &p.Sections[i], true
		}
	}
	return nil, false
}
