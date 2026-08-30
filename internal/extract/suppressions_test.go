package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Every suppression rule the profile writes, applied, and each held-back item
// carrying the bullet and line number that held it back. This is the audit
// trail: "it never appeared" is a much weaker claim than "it was seen and
// deliberately dropped, by this rule".
func TestSuppressionsApplyEveryProfileRule(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, err := tb.Suppressions()
	if err != nil {
		t.Fatalf("Suppressions: %v", err)
	}

	cases := []struct {
		source model.Source
		ref    string
		why    string
	}{
		{model.SourceEmail, "e-004", "newsletter"},
		{model.SourceEmail, "e-013", "FYI"},
		{model.SourceEmail, "e-016", "last word"},
		{model.SourceEmail, "e-010", "recruiter"},
		{model.SourceCalendar, "ev-gtm-sync", "accepted"},
	}
	for _, tc := range cases {
		found := false
		for _, s := range ss {
			if !cites(s, tc.source, tc.ref) {
				continue
			}
			found = true
			if !strings.Contains(strings.ToLower(s.Detail), strings.ToLower(tc.why)) {
				t.Errorf("%s:%s suppressed, but the reason does not mention %q:\n%s", tc.source, tc.ref, tc.why, s.Detail)
			}
			if !strings.Contains(s.Detail, tb.ProfilePath()+":") {
				t.Errorf("%s:%s suppression does not cite the profile line that caused it:\n%s", tc.source, tc.ref, s.Detail)
			}
		}
		if !found {
			t.Errorf("%s:%s was not suppressed (%s rule)", tc.source, tc.ref, tc.why)
		}
	}
}

// "Even Stratechery." The rule matches on the proper noun the user bothered to
// write down, not just on a label — which is what makes it work on a corpus
// where the label is missing.
func TestSuppressionsMatchProperNounsInTheRule(t *testing.T) {
	got := properNounTokens("Newsletters. Even the good ones. Even Stratechery.")
	if len(got) != 1 || got[0] != "stratechery" {
		t.Errorf("properNounTokens = %v, want just the name the user typed", got)
	}
	// Sentence-initial capitals are not names.
	if tokens := properNounTokens("Marketing emails are noise."); len(tokens) != 0 {
		t.Errorf("properNounTokens = %v, want none: every sentence starts with a capital", tokens)
	}
}

// The carve-out inside the recruiter rule: individually invisible, collectively
// a pattern, exactly as the profile asks.
func TestSuppressionsSurfaceTheRecruiterPattern(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).Suppressions()

	s, ok := findSignal(ss, "suppressions:pattern:apexsearch.example")
	if !ok {
		t.Fatalf("no recruiter pattern signal; got: %+v", ss)
	}
	if len(s.Citations) != 3 {
		t.Errorf("pattern cites %d messages, want all 3 that formed it", len(s.Citations))
	}
	if s.SectionHint != model.SectionPulse {
		t.Errorf("section = %s, want %s: the pattern is the story, not the mail", s.SectionHint, model.SectionPulse)
	}
	// Below the threshold there is no pattern, only silence.
	for _, id := range []string{"suppressions:pattern:stratechery.example"} {
		if _, found := findSignal(ss, id); found {
			t.Errorf("%s should not exist", id)
		}
	}
}

// The last-word rule stops at P0/P1. A P2 vendor thread the owner closed is
// suppressed; the P0 investor thread with the same shape is not.
func TestSuppressionsLastWordRuleStopsAtHighPriority(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	if _, ok := tb.supp.thread("t-print"); !ok {
		t.Error("a P2 thread the owner closed should be suppressed by the last-word rule")
	}
	if _, ok := tb.supp.thread("t-inflection"); ok {
		t.Error("a P0 investor thread must survive the last-word rule (the profile asks about quiet investor threads)")
	}
}

// A rule the parser cannot classify is inert and visible, never approximated.
func TestSuppressionsReportUnclassifiedRules(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	if got := tb.UnhandledSuppressions(); len(got) != 0 {
		t.Errorf("this profile is fully understood, but %d rule(s) were reported unhandled: %+v", len(got), got)
	}

	c := corpusOf(nil)
	c.Profile.Suppressions = []model.Rule{{Text: "Anything from the third floor.", Line: 9}}
	tb2 := toolboxOf(t, c, fixtureDay)
	unhandled := tb2.UnhandledSuppressions()
	if len(unhandled) != 1 || unhandled[0].Line != 9 {
		t.Errorf("an unrecognised rule should be reported inert, got %+v", unhandled)
	}
}

// With no profile there is nothing to suppress — and nothing crashes.
func TestSuppressionsWithoutAProfile(t *testing.T) {
	c := corpusOf([]model.Email{msg(t, "m-1", "th-1", "2026-08-28T09:00:00-07:00", "x", "hi", "hello")})
	c.Profile = nil
	ss, err := toolboxOf(t, c, fixtureDay).Suppressions()
	if err != nil {
		t.Fatalf("Suppressions: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("got %d suppressions with no profile: %+v", len(ss), ss)
	}
}
