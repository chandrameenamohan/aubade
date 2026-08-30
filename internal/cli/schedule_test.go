package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The design is a graded deliverable, so what it has to answer is asserted
// rather than trusted: the recommendation, both rejected alternatives with a
// reason each, where the secrets live, and what makes a re-run idempotent.
// Wording is free to change; the questions are not.
func TestScheduleDesignAnswersTheDeliverable(t *testing.T) {
	out, err := run(NewAubadeCmd(), "schedule", "--design")
	if err != nil {
		t.Fatalf("schedule --design failed: %v", err)
	}

	for _, want := range []string{
		"05:45",                // the recommended time
		"America/Los_Angeles",  // in the user's timezone, not the runner's
		"GitHub Actions",       // the recommendation
		"cron",                 // the mechanism
		"launchd",              // rejected: laptop asleep
		"asleep",               //   ... and the reason
		"cloud function",       // rejected: over-built at n=1
		"secrets",              // where credentials live
		"--no-llm",             // the zero-key degraded mode
		"digest-YYYY-MM-DD.md", // idempotency by date-stamped output
		"idempoten",            //   ... named as such
		"non-goal",             // and it is a design, not an implementation
	} {
		if !strings.Contains(out, want) {
			t.Errorf("schedule --design does not discuss %q", want)
		}
	}
}

// `aubade schedule` with no flag scheduled nothing, so it must not exit 0. An
// agent that asked aubade to schedule a job and got a document plus a zero exit
// code would reasonably report the job as scheduled.
func TestScheduleWithoutDesignIsAnError(t *testing.T) {
	out, err := run(NewAubadeCmd(), "schedule")
	if err == nil {
		t.Fatalf("bare `schedule` exited 0 with output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--design") {
		t.Errorf("error does not point at --design: %v", err)
	}
}

// An AI caller gets the design as a JSON envelope it can pull one field out of,
// rather than markdown it has to guess the shape of.
func TestScheduleDesignJSONEnvelope(t *testing.T) {
	out, err := run(NewAubadeCmd(), "schedule", "--design", "--json")
	if err != nil {
		t.Fatalf("schedule --design --json failed: %v", err)
	}

	var payload struct {
		OK       bool   `json:"ok"`
		Kind     string `json:"kind"`
		Doc      string `json:"doc"`
		Document string `json:"document"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out)
	}
	if !payload.OK || payload.Kind != "scheduling_design" {
		t.Errorf("unexpected envelope: ok=%v kind=%q", payload.OK, payload.Kind)
	}
	if payload.Doc != designDocPath {
		t.Errorf("envelope points at %q, want %q", payload.Doc, designDocPath)
	}
	if !strings.Contains(payload.Document, "## Scheduling design") {
		t.Error("the envelope does not carry the design itself")
	}
}

// The design exists twice on purpose — embedded in the binary so a shipped
// aubade can print it, and in DESIGN.md so a reader browsing the repo finds it
// where a reader looks. Two copies is a drift risk, so it gets the same
// treatment as the golden digests: a test that fails the moment they disagree.
func TestScheduleDesignMatchesDESIGN(t *testing.T) {
	path := filepath.Join("..", "..", designDocPath)
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", designDocPath, err)
	}
	if !strings.Contains(string(doc), strings.TrimSpace(scheduleDesign)) {
		t.Fatalf("%s no longer contains internal/cli/schedule_design.md verbatim.\n"+
			"They are one design in two places: paste the file's contents back into %s "+
			"(or edit the file, not the copy).", designDocPath, designDocPath)
	}
}
