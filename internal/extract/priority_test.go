package extract

import (
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The profile's "People who matter" list is the whole ranking policy, and it is
// written in two very different shapes: named people, and cohorts that name no
// mailbox at all. Both have to resolve, and neither may resolve by accident.
func TestPriorityResolvesProfileEntries(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	cases := []struct {
		name   string
		person model.Person
		labels []string
		want   model.Priority
		why    string
	}{
		{"named, display name", model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"}, nil, model.P0, "named in the profile"},
		{"named, mailbox spells it", model.Person{Email: "diane.okafor@okaforcapital.example"}, nil, model.P1, "first.last@ resolves the name"},
		{"cohort by domain", model.Person{Name: "Renee Tan", Email: "renee@halberd.example"}, nil, model.P1, "Halberd is a named reference customer"},
		{"cohort by domain, second name", model.Person{Name: "Dana Reyes", Email: "dana@northstar.example"}, nil, model.P1, "Northstar is in the same bullet"},
		{"cohort by label", model.Person{Name: "Ada Fern", Email: "ada@apexsearch.example"}, []string{"recruiter"}, model.P4, "the recruiter cohort"},
		{"machine floor", model.Person{Name: "Stratechery", Email: "newsletter@stratechery.example"}, []string{"newsletter"}, model.P4, "bulk mail floors at P4"},
		{"unknown human", model.Person{Name: "Dee Marlow", Email: "dee@printworks.example"}, nil, model.P2, "unnamed correspondents default to P2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tb.prio.Of(tc.person, tc.labels)
			if got.Priority != tc.want {
				t.Errorf("Of(%v) = %s, want %s (%s)", tc.person, got.Priority, tc.want, tc.why)
			}
		})
	}
}

// Matching is word-equality on stems, never substring: "Sam" must not become
// "Samantha", and "north" must not become "northstar".
func TestPriorityDoesNotMatchOnSubstrings(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	for _, p := range []model.Person{
		{Name: "Samantha Prakash", Email: "samantha@elsewhere.example"},
		{Name: "Nora Halberdine", Email: "nora@halberdine.example"},
	} {
		if got := tb.prio.Of(p, nil); got.Priority != defaultPriority {
			t.Errorf("Of(%v) = %s, want the default %s — a substring is not an identity",
				p, got.Priority, defaultPriority)
		}
	}
}

// A signal that re-ranked someone's morning says which of their own bullets did
// it. The profile is settings, not data, so it is never a Citation — but it is
// always named.
func TestPriorityCitesTheProfileLine(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	m := tb.prio.Of(model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"}, nil)
	if m.Person == nil {
		t.Fatal("Marcus should resolve to a profile entry")
	}
	if got := m.Cited("profile.md"); got != "P0 per profile.md:19" {
		t.Errorf("Cited() = %q, want the priority and the line it came from", got)
	}

	def := tb.prio.Of(model.Person{Email: "nobody@example.test"}, nil)
	if got := def.Cited("profile.md"); got == "" || def.Person != nil {
		t.Errorf("an unmatched correspondent should say so: %q", got)
	}
}

// Conditional entries ("P0 during the raise, P2 otherwise") keep every priority
// the bullet mentioned. Flattening them would lose the condition while
// pretending to be precise; the default is the first one written.
func TestPriorityKeepsConditionalEntriesIntact(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	ben, ok := tb.corpus.Profile.PersonByName("Ben Schaffer")
	if !ok {
		t.Fatal("Ben Schaffer missing from the parsed profile")
	}
	if !ben.Conditional || len(ben.Priorities) != 2 {
		t.Errorf("expected a conditional entry with two priorities, got %+v", ben)
	}
	if got := tb.prio.Of(model.Person{Name: "Ben Schaffer", Email: "ben@wsgr.com"}, nil); got.Priority != model.P0 {
		t.Errorf("default priority = %s, want the first one the bullet names", got.Priority)
	}
}

// With no profile, everything is a default and nothing crashes.
func TestPriorityWithoutAProfile(t *testing.T) {
	m := newPriorityMap(nil)
	if got := m.Of(model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"}, nil); got.Priority != defaultPriority {
		t.Errorf("Of() = %s with no profile, want %s", got.Priority, defaultPriority)
	}
}

func TestLooksLikePersonName(t *testing.T) {
	cases := map[string]bool{
		"Sam Park":                     true,
		"Priya Iyer":                   true,
		"Marcus Webb":                  true,
		"Recruiters cold-emailing me":  false,
		"The procurement leads at X":   false,
		"Halberd Manufacturing, Inc":   false,
		"A Very Long Name Indeed Here": false,
	}
	for name, want := range cases {
		if got := looksLikePersonName(name); got != want {
			t.Errorf("looksLikePersonName(%q) = %v, want %v", name, got, want)
		}
	}
}
