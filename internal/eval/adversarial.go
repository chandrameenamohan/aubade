package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner"
)

// The adversarial suite: traps this repository did not write.
//
// Every task in the regression suite was authored by the same people who wrote
// the extractors, which is the structural weakness of the whole exam. An eval
// written by the engine's authors tests the failures they thought of, and it
// saturates the moment the engine handles those (EVAL-PRINCIPLES #16). #20 is
// the answer — "assume there are problems, find them; this is a bug hunt, not a
// confirmation step" — so a model is pointed at the corpus, the profile and the
// existing catalog, and asked for situations that are *not* in it.
//
// Four properties make the difference between that and a party trick:
//
//  1. **The original corpus is never written to.** The suite copies the data
//     directory, injects into the copy, and grades the copy. `--adversarial` on
//     a pinned corpus leaves it byte-identical, so a golden digest, a reference
//     solution and a repeat run all still mean what they meant.
//  2. **Nothing unvalidated reaches the corpus.** An authored scenario is
//     compiled against the contract in authored.go — real category, real
//     extractor, no id collisions, dates inside the window, keywords quotable
//     from its own evidence — and a scenario that fails is rejected with its
//     reasons and re-asked exactly once. A model gets one correction, not an
//     unbounded loop on someone's account.
//  3. **planted_refs are derived, never asked for.** The citations come from
//     the artifacts the scenario emitted, so an authored trap cannot cite
//     evidence it did not plant — the same invariant datagen's Scenario
//     signature enforces for the shipped catalog.
//  4. **It is a capability suite, so it never gates.** A miss here is news
//     about the engine's coverage, not a regression: the tasks did not exist
//     five minutes ago, they are non-deterministic between runs, and a gate
//     that goes red because a model invented a hard question is a gate people
//     learn to bypass (VERIFICATION.md §2). It is out of `make check` and it
//     never touches the exit code.

// AdversarialScenarios is how many new traps the suite asks for. Three is
// enough for the mix the prompt asks for — some that must surface, some that
// must not — and every one of them is a paid model call plus a corpus copy.
const AdversarialScenarios = 3

// AdversarialDir is where the injected copy and its graded page are written,
// relative to --out.
const AdversarialDir = "adversarial"

// AuthoredFile is the model's accepted answer, saved beside the copy it was
// injected into. Reading transcripts is how you find out whether a grader is
// working (#18), and for this suite the authored scenarios *are* the
// transcript.
const AuthoredFile = "authored.json"

// authorBudget caps one authoring call. It is well past AskBudget because the
// answer is a corpus fragment — several emails with real bodies — rather than a
// one-word vote.
const authorBudget = 5 * time.Minute

// Adversarial is one adversarial run.
type Adversarial struct {
	// Skipped and SkipReason record a suite that could not run. As with the
	// capability suite, a skip is reported as loudly as a failure.
	Skipped    bool
	SkipReason string

	// Author names the runner that wrote the scenarios.
	Author string

	// Want is how many scenarios were asked for, Attempts how many times the
	// model was asked (one, or two when the first answer was rejected).
	Want     int
	Attempts int

	// Rejections are the scenarios that did not survive validation, with the
	// reason each was thrown out. They are the most interesting output of the
	// suite when it goes wrong, and they are what the retry was told.
	Rejections []Rejection

	// Traps are the accepted, injected tasks.
	Traps datagen.Traps

	// Dir is the injected copy of the corpus.
	Dir string

	// Result grades the authored tasks against the page composed from the copy,
	// and Control re-grades the original answer key over the same page — a task
	// that broke because the injection disturbed the corpus is not a miss on the
	// new trap.
	Result  *Result
	Control *Result

	// Err is why the run produced no grade at all.
	Err error
}

// Caught reports how many authored tasks the engine caught, out of how many
// were injected.
func (a *Adversarial) Caught() (caught, total int) {
	if a == nil || a.Result == nil {
		return 0, 0
	}
	return a.Result.Score()
}

// Rejection is one authored scenario the contract refused.
type Rejection struct {
	// Attempt is 1 for the first answer, 2 for the retry.
	Attempt int

	// ID is the scenario's own id, or a positional label when it had none.
	ID string

	// Reason is every rule it broke.
	Reason string
}

// AdversarialInput is one adversarial run's world.
type AdversarialInput struct {
	// Author is the runner that writes the scenarios. Nil skips the suite.
	Author runner.Runner

	// DataDir is the corpus to copy. It is opened read-only and never written.
	DataDir string

	// WorkDir is where the copy and the graded page go.
	WorkDir string

	// Corpus, Traps, Today and Loc are the exam as it stands, and are what an
	// authored scenario is validated against.
	Corpus *model.Corpus
	Traps  datagen.Traps
	Today  time.Time
	Loc    *time.Location

	// Want is how many scenarios to ask for; zero means AdversarialScenarios.
	Want int
}

// RunAdversarial authors new traps, injects them into a copy of the corpus, and
// re-runs the deterministic harness over the copy.
//
// It returns a report rather than an error in every case a run can fail: an
// adversarial suite that could not author, could not inject or could not
// compose is a suite with nothing to say, and the card says so. Only the caller
// decides what to print, and nothing here reaches the exit code.
func RunAdversarial(ctx context.Context, in AdversarialInput) *Adversarial {
	want := in.Want
	if want <= 0 {
		want = AdversarialScenarios
	}
	a := &Adversarial{Want: want}

	if in.Author == nil {
		a.Skipped = true
		a.SkipReason = "no model runner answered a probe on this machine, so there was nobody to author new traps"
		return a
	}
	a.Author = in.Author.Name()

	ns := newNamespace(in.Corpus, in.Traps, in.Today, in.Loc)
	accepted := author(ctx, in, ns, a)
	if len(accepted) == 0 {
		a.Err = fmt.Errorf("%s produced no usable scenario in %d attempt(s); %d were rejected — the reasons are on the card",
			a.Author, a.Attempts, len(a.Rejections))
		return a
	}

	a.Dir = filepath.Join(in.WorkDir, "data")
	if err := copyTree(in.DataDir, a.Dir); err != nil {
		a.Err = err
		return a
	}
	delta := authoredPlan(accepted)
	if err := datagen.Inject(a.Dir, delta); err != nil {
		a.Err = fmt.Errorf("injecting the authored traps: %w", err)
		return a
	}
	a.Traps = delta.Traps
	if err := writeAuthored(filepath.Join(in.WorkDir, AuthoredFile), accepted); err != nil {
		a.Err = err
		return a
	}

	corpus, err := model.LoadCorpus(ctx, localfs.New(a.Dir))
	if err != nil {
		a.Err = fmt.Errorf("the injected corpus does not load: %w", err)
		return a
	}
	page, err := Compose(corpus, in.Today, in.Loc, "")
	if err != nil {
		a.Err = fmt.Errorf("composing the digest over the injected corpus: %w", err)
		return a
	}
	if err := writeArtifacts(in.WorkDir, page); err != nil {
		a.Err = err
		return a
	}

	a.Result = Grade(a.Traps, page)
	a.Control = Grade(in.Traps, page)
	return a
}

// author asks for scenarios, keeps the ones that compile, and re-asks once for
// the ones that did not.
//
// One retry, and one only. The rejection reasons are specific enough to act on,
// so a model that cannot use them will not be helped by a third go; and an
// authoring loop that keeps spending until it succeeds is a harness with an
// unbounded bill.
func author(ctx context.Context, in AdversarialInput, ns *namespace, a *Adversarial) []authored {
	var accepted []authored

	for attempt := 1; attempt <= 2; attempt++ {
		missing := a.Want - len(accepted)
		if missing <= 0 {
			break
		}
		var feedback []Rejection
		if attempt > 1 {
			feedback = a.Rejections
			if len(feedback) == 0 {
				break // nothing was rejected; the model simply returned fewer
			}
		}

		a.Attempts = attempt
		raw, err := in.Author.Ask(ctx, runner.Question{
			Prompt: authorPrompt(in, missing, feedback),
			Schema: runner.Schema{Name: "adversarial-scenarios", JSON: adversarialSchema},
			Budget: authorBudget,
		})
		if err != nil {
			// A runner that did not answer is recorded like any other rejected
			// attempt rather than as a fatal error, because an earlier attempt may
			// already have produced scenarios worth injecting. Whether the run has
			// anything to say is decided once, by the caller, on what survived.
			a.Rejections = append(a.Rejections, Rejection{
				Attempt: attempt,
				ID:      "(no answer)",
				Reason:  err.Error(),
			})
			return accepted
		}

		scenarios, err := decodeScenarios(raw)
		if err != nil {
			a.Rejections = append(a.Rejections, Rejection{Attempt: attempt, ID: "(whole answer)", Reason: err.Error()})
			continue
		}
		for i, s := range scenarios {
			if len(accepted) >= a.Want {
				break
			}
			built, err := compile(s, ns)
			if err != nil {
				a.Rejections = append(a.Rejections, Rejection{
					Attempt: attempt,
					ID:      scenarioLabel(s.ID, i),
					Reason:  joinLines(err),
				})
				continue
			}
			ns.claim(built)
			accepted = append(accepted, built)
		}
	}
	return accepted
}

// decodeScenarios reads the answer envelope. A model that answers off-schema
// has not authored a hard trap, it has failed to answer, and the difference
// belongs on the card.
//
// Unknown fields are ignored rather than rejected, which is the same posture
// localfs takes towards a corpus written by a newer generator: the contract
// that matters is compile()'s, and throwing away three usable scenarios because
// one of them carried a field we do not read would be a rejection that teaches
// nothing.
func decodeScenarios(raw json.RawMessage) ([]AuthoredScenario, error) {
	var answer struct {
		Scenarios []AuthoredScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("the answer does not match the scenario schema: %w", err)
	}
	if len(answer.Scenarios) == 0 {
		return nil, fmt.Errorf("the answer carries no scenarios")
	}
	return answer.Scenarios, nil
}

func scenarioLabel(id string, i int) string {
	if s := strings.TrimSpace(id); s != "" {
		return clip(s, 60)
	}
	return fmt.Sprintf("scenarios[%d]", i)
}

// joinLines flattens a joined error into one readable list.
func joinLines(err error) string {
	parts := strings.Split(err.Error(), "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, "; ")
}

// writeAuthored saves the accepted scenarios beside the corpus they were
// injected into.
func writeAuthored(path string, list []authored) error {
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"trap":   a.Trap,
			"emails": a.Emails,
			"events": a.Events,
			"notes":  a.Notes,
			"tasks":  a.Tasks,
		})
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the authored scenarios: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// writeArtifacts puts the adversarial run's page and fact base on disk, in the
// same two file names every other trial uses — so a reader can open it, and so
// LoadArtifacts can read it back.
func writeArtifacts(dir string, a *Artifacts) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, digest.DigestFile), []byte(a.Digest), 0o644); err != nil {
		return fmt.Errorf("cannot write the adversarial digest: %w", err)
	}
	return extract.WriteSignals(filepath.Join(dir, extract.SignalsFile), a.Signals)
}

// copyTree copies a corpus directory into a fresh one.
//
// It is the whole of the "original untouched" promise, so it only ever reads
// from src. The destination is cleared first rather than merged into: a second
// adversarial run inheriting the first one's injected traps would grade last
// run's questions as this run's, and would collide with them besides.
func copyTree(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("cannot clear %s: %w", dst, err)
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dst, err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and devices are not corpus
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		return os.WriteFile(target, raw, 0o644)
	})
}
