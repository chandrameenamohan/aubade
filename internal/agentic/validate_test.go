package agentic

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The validator is where "the model cannot fabricate" stops being a claim in a
// design document, so it gets the sharpest tests in this package: a page that
// cites something the toolbox never produced must be caught, and a page that
// cites nothing must be caught too.

func TestFactBaseAcceptsAPageBuiltFromIt(t *testing.T) {
	fb := NewFactBase(testSignals())
	if fb.Size() != 3 {
		t.Fatalf("Size() = %d, want the 3 distinct citations in the fact base", fb.Size())
	}

	page := strings.Join([]string{
		"# Daily Digest — Sunday, August 30, 2026",
		"## Urgent To-Do Today",
		"- **Answer Marcus.** The term sheet expires today. [email:e-001]",
		"- **The board sync collides with the customer call.** [calendar:evt-7] [note:notes/kickoff.md]",
	}, "\n")

	if v := fb.Validate(page); len(v) != 0 {
		t.Fatalf("a page built from the fact base was rejected: %v", v)
	}
}

// The headline test for this bead: one invented ref among many real ones is
// caught, and it is caught by ref rather than by luck.
func TestFactBaseCatchesAnInjectedFakeCitation(t *testing.T) {
	fb := NewFactBase(testSignals())

	page := strings.Join([]string{
		"# Daily Digest — Sunday, August 30, 2026",
		"- **Answer Marcus.** The term sheet expires today. [email:e-001]",
		"- **Legal signed off on the SOC2 report last night.** [email:e-999]",
	}, "\n")

	v := fb.Validate(page)
	if len(v) != 1 {
		t.Fatalf("violations = %v, want exactly the fabricated one", v)
	}
	if v[0].Kind != violationUnknown || v[0].Ref != "email:e-999" {
		t.Errorf("violation = %+v, want the fabricated ref named", v[0])
	}
	if !strings.Contains(v[0].String(), "Legal signed off") {
		t.Errorf("violation %q should quote the line it was found on", v[0].String())
	}
}

// A page with no receipts anywhere is not a page that happened to cite nothing;
// it is a page whose every line is unverifiable.
func TestFactBaseRejectsAPageWithNoCitationsAtAll(t *testing.T) {
	fb := NewFactBase(testSignals())

	v := fb.Validate("# Daily Digest\n\nEverything looks fine this morning.\n")
	if len(v) != 1 || v[0].Kind != violationNone {
		t.Fatalf("violations = %v, want the uncited page rejected", v)
	}
}

// The citation pattern is closed over the four sources on purpose: a looser one
// would read ordinary markdown as a fabrication, and a checker that cries wolf
// gets switched off.
func TestValidatorDoesNotMistakeMarkdownForACitation(t *testing.T) {
	fb := NewFactBase(testSignals())

	page := "# Daily Digest\n\n- See [the thread](https://example.test) and [TODO: follow up]. [email:e-001]\n"
	if v := fb.Validate(page); len(v) != 0 {
		t.Fatalf("violations = %v, want links and bracketed prose left alone", v)
	}
}

// Validate reads the id, Resolve renders the name. The order matters: the id is
// the thing that can be checked, and swapping them would mean checking the half
// nobody wrote down.
func TestResolveRendersCheckedRefsAsReadableSpans(t *testing.T) {
	corpus := testCorpus()
	label := digest.NewLabeler(corpus, model.Location())

	got := Resolve("- **Answer Marcus.** The term sheet expires today. [email:e-001]", label)
	if !strings.Contains(got, "*[email: Marcus,") {
		t.Errorf("resolved line = %q, want the reader-facing span", got)
	}
	if strings.Contains(got, "e-001") {
		t.Errorf("resolved line = %q, want the id replaced by who said it and when", got)
	}
}

// A ref that does not resolve against the corpus renders as itself rather than
// vanishing: a citation nobody can follow is a bug worth seeing, and a line with
// its receipt silently removed is a claim with no receipt.
func TestResolveKeepsAnUnresolvableRefVisible(t *testing.T) {
	label := digest.NewLabeler(testCorpus(), model.Location())
	got := Resolve("cited [task:t-404]", label)
	if !strings.Contains(got, "t-404") {
		t.Errorf("resolved line = %q, want the raw ref kept", got)
	}
}
