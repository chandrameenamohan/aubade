package eval

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The graders are tested against hand-built signals rather than against the
// generated corpus, for the same reason the extractors are: a grader graded only
// on data that already passes proves that today looks like yesterday. What has
// to be pinned here is the *semantics* — which combinations pass, which fail,
// and which of the two the suppression audit record is.

func cite(source model.Source, ref string) model.Citation {
	return model.Citation{Source: source, Ref: ref}
}

// signal is a valid signal with the fields a grader reads.
func signal(kind, section string, cites ...model.Citation) model.Signal {
	return model.Signal{
		ID:          kind + ":" + cites[0].Ref,
		Kind:        kind,
		Priority:    model.P1,
		Title:       "a signal",
		Detail:      `held back — "the rule text" (profile.md:12)`,
		Citations:   cites,
		SectionHint: section,
		Confidence:  model.Certain,
	}
}

func positiveTrap() datagen.Trap {
	return datagen.Trap{
		ID: "positive", Kind: datagen.CommitmentSlip, Description: "d", MustSurface: true,
		Expect:      datagen.Expect{SignalKind: model.KindCommitments, Keywords: []string{"cap table"}},
		PlantedRefs: []model.Citation{cite(model.SourceEmail, "m-1")},
	}
}

func negativeTrap() datagen.Trap {
	return datagen.Trap{
		ID: "negative", Kind: datagen.Newsletter, Description: "d", MustSurface: false,
		Expect:      datagen.Expect{SignalKind: model.KindSuppressions, Keywords: []string{"Stratechery"}},
		PlantedRefs: []model.Citation{cite(model.SourceEmail, "m-2")},
	}
}

func page(text string) *Artifacts { return &Artifacts{Digest: text} }

func gradeOne(t *testing.T, trap datagen.Trap, a *Artifacts) TrapResult {
	t.Helper()
	res := Grade(datagen.Traps{trap}, a)
	r, ok := res.Get(trap.ID)
	if !ok {
		t.Fatalf("no result for trap %s", trap.ID)
	}
	return r
}

// A positive task needs both halves. A signal nobody rendered is a fact the
// user never saw; a keyword with no signal behind it is prose with no receipt.
func TestPositiveTaskNeedsSignalAndKeyword(t *testing.T) {
	trap := positiveTrap()
	found := signal(model.KindCommitments, model.SectionUrgentToday, cite(model.SourceEmail, "m-1"))

	cases := []struct {
		name    string
		art     *Artifacts
		want    bool
		wantSub string
	}{
		{
			name: "both halves",
			art:  &Artifacts{Digest: "send Marcus the cap table", Signals: model.Signals{found}},
			want: true,
		},
		{
			name:    "signal but the page never says it",
			art:     &Artifacts{Digest: "nothing needs you today", Signals: model.Signals{found}},
			wantSub: "lost in the render",
		},
		{
			name:    "page says it with no signal behind it",
			art:     page("send Marcus the cap table"),
			wantSub: "no signal cites any of its planted refs",
		},
		{
			name:    "a signal about something else entirely",
			art:     &Artifacts{Digest: "send Marcus the cap table", Signals: model.Signals{signal(model.KindCommitments, model.SectionUrgentToday, cite(model.SourceEmail, "m-99"))}},
			wantSub: "no signal cites any of its planted refs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gradeOne(t, trap, tc.art)
			if r.Passed != tc.want {
				t.Errorf("passed = %v, want %v (%s)", r.Passed, tc.want, r.Reason)
			}
			if tc.wantSub != "" && !strings.Contains(r.Reason, tc.wantSub) {
				t.Errorf("reason %q does not name the problem (%q)", r.Reason, tc.wantSub)
			}
		})
	}
}

// The keyword is matched case-insensitively and as a substring, because the
// page is prose: it capitalises a sentence and pluralises a noun, and neither
// is a recall failure.
func TestKeywordMatchingToleratesProse(t *testing.T) {
	trap := positiveTrap()
	trap.Expect.Keywords = []string{"expense report"}
	found := signal(model.KindDispatchables, model.SectionDecisions, cite(model.SourceEmail, "m-1"))

	for _, text := range []string{
		"Three Expense Reports need your approval",
		"expense report waiting",
	} {
		r := gradeOne(t, trap, &Artifacts{Digest: text, Signals: model.Signals{found}})
		if !r.Passed {
			t.Errorf("page %q did not match the keyword: %s", text, r.Reason)
		}
	}
}

// The expected extractor is reported, never enforced: an engine that finds the
// right item by a route the answer key did not anticipate has not failed.
func TestExpectedExtractorIsReportedNotEnforced(t *testing.T) {
	trap := positiveTrap()
	other := signal(model.KindQuietThreads, model.SectionUrgentToday, cite(model.SourceEmail, "m-1"))

	r := gradeOne(t, trap, &Artifacts{Digest: "the cap table is late", Signals: model.Signals{other}})
	if !r.Passed {
		t.Fatalf("a trap surfaced by another extractor must still pass: %s", r.Reason)
	}
	if !strings.Contains(r.Reason, "the answer key expected commitments") {
		t.Errorf("the mismatch must be reported: %q", r.Reason)
	}
}

// The suppression audit record is the record of NOT surfacing something. A
// negative task cited only by one has passed, and the card should say so.
func TestAuditRecordIsNotSurfacing(t *testing.T) {
	trap := negativeTrap()
	audit := signal(model.KindSuppressions, model.SectionHonesty, cite(model.SourceEmail, "m-2"))

	r := gradeOne(t, trap, &Artifacts{
		// The page names the newsletter inside the rule that suppressed it —
		// which is exactly why the negative half does not grade keywords.
		Digest:  `Held back 6 items. "Newsletters. Even the good ones. Even Stratechery."`,
		Signals: model.Signals{audit},
	})
	if !r.Passed {
		t.Fatalf("an item held back by the profile must pass: %s", r.Reason)
	}
	if !r.Suppressed || r.Rule != "the rule text" {
		t.Errorf("the rule that held it back must be reported, got suppressed=%v rule=%q", r.Suppressed, r.Rule)
	}
}

// The exception is narrow: the same kind in any other section is a real
// finding, and a negative task caught by one has failed.
func TestSuppressionOutsideTheHonestySectionIsSurfacing(t *testing.T) {
	trap := negativeTrap()
	pattern := signal(model.KindSuppressions, model.SectionPulse, cite(model.SourceEmail, "m-2"))

	r := gradeOne(t, trap, &Artifacts{Digest: "a page", Signals: model.Signals{pattern}})
	if r.Passed {
		t.Fatalf("a suppressions signal in the pulse section is surfacing: %s", r.Reason)
	}
	if !strings.Contains(r.Reason, "must not appear") {
		t.Errorf("reason does not name the failure: %q", r.Reason)
	}
}

// A negative task nothing ever considered passes, and the card distinguishes it
// from one the profile deliberately held back. They are both passes and they
// are not the same news.
func TestNegativeTaskReportsHowItStayedOut(t *testing.T) {
	r := gradeOne(t, negativeTrap(), page("a page with nothing about it"))
	if !r.Passed {
		t.Fatalf("nothing claimed it, so it passes: %s", r.Reason)
	}
	if r.Suppressed || r.Reason != "no extractor claimed it" {
		t.Errorf("an unexamined pass must say so, got %q", r.Reason)
	}
}

// A trial that produced nothing fails every task rather than crashing: "the
// digest scored 0" is a score, and the caller that could not produce artefacts
// reports why separately.
func TestGradingNothingFailsEverything(t *testing.T) {
	res := Grade(datagen.Traps{positiveTrap(), negativeTrap()}, nil)
	passed, total := res.Score()
	if total != 2 {
		t.Fatalf("graded %d tasks, want 2", total)
	}
	if passed != 1 {
		t.Errorf("with no artefacts the positive task must fail and the negative must pass, got %d passed", passed)
	}
	if res.Passed() {
		t.Error("a run with no artefacts must not report a pass")
	}
}

// The mode is read off the page's own footer, so a scorecard cannot claim to
// have graded an agentic page that quietly fell back.
func TestModeIsReadFromThePage(t *testing.T) {
	cases := map[string]string{
		"*Composed by `aubade digest --no-llm`: 12 signals*":     "no-llm",
		"*Composed by claude in agentic mode. Consensus on.*":    "agentic",
		"> This page was not composed by the model; it cited a…": "agentic-fallback",
		"no footer at all": "unknown",
	}
	for text, want := range cases {
		if got := page(text).Mode(); got != want {
			t.Errorf("Mode(%q) = %q, want %q", text, got, want)
		}
	}
}
