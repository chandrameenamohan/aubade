package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// What a trial leaves on disk, and how the harness reads it back.
//
// The harness grades files rather than calling into the packages that wrote
// them, and that is deliberate: `make check` drives the real binaries, so what
// gets graded is the thing that ships. A grader that re-composed the page
// in-process would be grading a second implementation of the product and would
// go green over a broken `aubade digest`.

// TranscriptFile is the runner's own record of the loop, written beside the
// page in agentic mode. The name is duplicated from the CLI rather than
// imported because internal/cli imports this package; the constant is asserted
// against it in the CLI's own tests.
const TranscriptFile = "transcript.jsonl"

// Artifacts is one trial's output directory, loaded.
type Artifacts struct {
	// Dir is the trial's own directory. Every trial gets its own (#11).
	Dir string

	// Digest is the page as written, and Signals is the fact base it was
	// composed from.
	Digest  string
	Signals model.Signals

	// Transcript is the raw runner transcript, empty in `--no-llm` mode where
	// there is no loop to record.
	Transcript string
}

// LoadArtifacts reads one trial directory.
//
// A missing digest or a missing signals.json is an error rather than an empty
// grade: "the digest scored 0" and "there is no digest" are different findings,
// and only one of them is about the engine.
func LoadArtifacts(dir string) (*Artifacts, error) {
	a := &Artifacts{Dir: dir}

	page, err := os.ReadFile(filepath.Join(dir, digest.DigestFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read the digest in %s: %w", dir, err)
	}
	a.Digest = string(page)

	a.Signals, err = extract.ReadSignals(filepath.Join(dir, extract.SignalsFile))
	if err != nil {
		return nil, err
	}

	// The transcript is optional by contract: only agentic trials have one.
	if raw, err := os.ReadFile(filepath.Join(dir, TranscriptFile)); err == nil {
		a.Transcript = string(raw)
	}
	return a, nil
}

// Mode reads how the page says it was composed. The footer states it in prose
// for the reader; this is the harness noticing the same sentence, so a
// scorecard cannot claim to be grading an agentic page that quietly fell back
// to the deterministic composer.
func (a *Artifacts) Mode() string {
	switch {
	case a == nil:
		return ""
	case strings.Contains(a.Digest, "was not composed by"):
		return "agentic-fallback"
	case strings.Contains(a.Digest, "in agentic mode"):
		return "agentic"
	case strings.Contains(a.Digest, "--no-llm"):
		return "no-llm"
	default:
		return "unknown"
	}
}

// LoadTraps reads the answer key that ships beside the corpus.
func LoadTraps(dataDir string) (datagen.Traps, error) {
	path := filepath.Join(dataDir, datagen.TrapsFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the answer key: %w\nrun `aubade-lab generate --out %s` first", err, dataDir)
	}
	var traps datagen.Traps
	if err := json.Unmarshal(raw, &traps); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", path, err)
	}
	if err := traps.Validate(); err != nil {
		return nil, fmt.Errorf("%s is not a valid answer key: %w", path, err)
	}
	if len(traps) == 0 {
		return nil, fmt.Errorf("%s contains no traps; there is nothing to grade", path)
	}
	return traps, nil
}
