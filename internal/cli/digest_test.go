package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
)

// What is under test here is the wiring — flags, files written, output shape,
// exit behaviour. The page itself is graded in internal/digest against its own
// fixtures and its committed goldens.

// digestArgs runs the digest over the shared test corpus into a temp dir.
func digestArgs(t *testing.T, rest ...string) (outDir string, args []string) {
	t.Helper()
	outDir = t.TempDir()
	return outDir, append(rest, "--data", corpusDir, "--today", corpusDay, "--out", outDir)
}

// The headline behaviour: a full page from local files, on stdout and on disk,
// with the signals it was composed from beside it.
func TestDigestNoLLMWritesThePageAndItsFactBase(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	outDir, args := digestArgs(t, "digest", "--no-llm")

	out, err := run(NewAubadeCmd(), args...)
	if err != nil {
		t.Fatalf("digest --no-llm: %v\n%s", err, out)
	}

	page, err := os.ReadFile(filepath.Join(outDir, digest.DigestFile))
	if err != nil {
		t.Fatalf("no digest written: %v", err)
	}
	md := string(page)
	for _, want := range []string{
		"# Daily Digest — ",
		"## If there is one thing you must do right now:",
		"## Urgent To-Do Today",
		"## Calendar & Personal",
		"## Honesty",
		"no model and no network in the loop",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the written page is missing %q", want)
		}
	}
	if !strings.Contains(out, md) {
		t.Error("the human run should print the page it wrote")
	}

	// The audit trail lands beside it, in the shape the eval harness reads.
	signals, err := extract.ReadSignals(filepath.Join(outDir, extract.SignalsFile))
	if err != nil {
		t.Fatalf("no signals.json beside the digest: %v", err)
	}
	if len(signals) == 0 {
		t.Error("signals.json is empty")
	}
}

// Same corpus, same --today, byte-identical file. The committed golden and the
// whole trap eval rest on this holding through the CLI, not only in the library.
func TestDigestIsReproducibleThroughTheCLI(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")

	read := func() string {
		outDir, args := digestArgs(t, "digest", "--no-llm")
		if _, err := run(NewAubadeCmd(), args...); err != nil {
			t.Fatalf("digest: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(outDir, digest.DigestFile))
		if err != nil {
			t.Fatalf("read digest: %v", err)
		}
		return string(data)
	}
	if first, second := read(), read(); first != second {
		t.Error("two runs over the same corpus and the same --today produced different pages")
	}
}

// An AI caller gets the run as JSON, page included, with no flag at all.
func TestDigestSpeaksJSONToAnAgentCaller(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")
	_, args := digestArgs(t, "digest", "--no-llm")

	out, err := run(NewAubadeCmd(), args...)
	if err != nil {
		t.Fatalf("digest: %v\n%s", err, out)
	}

	var payload struct {
		OK      bool   `json:"ok"`
		Mode    string `json:"mode"`
		Path    string `json:"path"`
		Signals string `json:"signals"`
		Digest  string `json:"digest"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("agent-mode output is not JSON: %v\n%s", err, out)
	}
	if !payload.OK || payload.Mode != digest.ModeNoLLM {
		t.Errorf("unexpected envelope: %+v", payload)
	}
	if !strings.HasPrefix(payload.Digest, "# Daily Digest — ") {
		t.Errorf("the envelope should carry the page itself, got %.60q", payload.Digest)
	}
	if payload.Path == "" || payload.Signals == "" {
		t.Error("the envelope should name both files it wrote")
	}
}

// Agentic mode is not built. It says so and names its bead rather than quietly
// composing the page some other way.
func TestDigestWithoutNoLLMIsAnHonestStub(t *testing.T) {
	_, args := digestArgs(t, "digest")

	_, err := run(NewAubadeCmd(), args...)
	var se *StubError
	if !errors.As(err, &se) {
		t.Fatalf("expected a *StubError, got %T (%v)", err, err)
	}
	if !strings.Contains(se.Error(), "--no-llm") {
		t.Errorf("the stub should point at the mode that does work: %q", se.Error())
	}
}

// --customize reshapes the compose stage and --no-llm has no compose stage to
// reshape (SPEC §6). Saying so beats ignoring one of the two flags.
func TestCustomizeWithNoLLMIsRefused(t *testing.T) {
	_, args := digestArgs(t, "digest", "--no-llm", "--customize", "prompt.md")

	_, err := run(NewAubadeCmd(), args...)
	if err == nil {
		t.Fatal("--customize --no-llm should be an error")
	}
	if !strings.Contains(err.Error(), "--customize") || !strings.Contains(err.Error(), "--no-llm") {
		t.Errorf("the message should name both flags: %v", err)
	}
}

// The digest binds to the same corpus contract as the toolbox: an absent corpus
// is an error, and a malformed --today stops the run rather than falling back
// to the clock.
func TestDigestValidatesItsInputsLikeTheToolbox(t *testing.T) {
	empty := t.TempDir()

	if _, err := run(NewAubadeCmd(), "digest", "--no-llm", "--data", filepath.Join(empty, "nope")); err == nil ||
		!strings.Contains(err.Error(), "no corpus at") {
		t.Errorf("an absent corpus should be a loud error, got %v", err)
	}
	if _, err := run(NewAubadeCmd(), "digest", "--no-llm", "--data", corpusDir, "--today", "31/08/2026"); err == nil ||
		!strings.Contains(err.Error(), "invalid --today") {
		t.Errorf("a malformed --today should stop the run, got %v", err)
	}
}
