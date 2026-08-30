package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// The adversarial suite writes into a corpus, which makes it the one part of
// the harness that could damage the exam. Most of what follows is about that:
// the original bytes, the retry budget, and the refusal to let an unvalidated
// scenario near the data.
//
// Nothing here calls a real model. The scenarios are scripted, so what is under
// test is the contract and the injection rather than a model's taste.

// intPtr is the due-date optionality the schema models.
func intPtr(n int) *int { return &n }

func person(name, addr string) model.Person { return model.Person{Name: name, Email: addr} }

func expect(kind string, keywords ...string) datagen.Expect {
	return datagen.Expect{SignalKind: kind, Keywords: keywords}
}

// aNegativeScenario is a vendor blast that must stay out of the digest. It is
// the scenario the tests grade against, because "the engine correctly ignored a
// marketing email" is a verdict that does not move between runs.
func aNegativeScenario() AuthoredScenario {
	return AuthoredScenario{
		ID:          "adv-vendor-blast",
		Kind:        "vendor-marketing",
		Description: "An unsolicited platform-pricing blast from a vendor Avery has no relationship with.",
		MustSurface: false,
		Expect:      expect("suppressions", "Northwind Analytics"),
		Emails: []AuthoredEmail{{
			ID:        "adv-e-northwind-1",
			ThreadID:  "adv-t-northwind",
			DayOffset: -12,
			Hour:      9,
			Minute:    14,
			From:      person("Northwind Analytics", "hello@northwind-analytics.example"),
			To:        []model.Person{person("Avery Chen", "avery@tessera.io")},
			Subject:   "Northwind Analytics — Q3 platform pricing update",
			Body:      "Hi there,\n\nWe have refreshed our Q3 pricing tiers. Northwind Analytics now bundles warehouse sync at no extra cost. No action needed — this note is for your records.\n\n— The Northwind Analytics team",
			Labels:    []string{"marketing"},
		}},
	}
}

// aPositiveScenario is a promise with a date on it. It is injected and graded,
// but no test asserts the verdict: whether the engine catches a trap it has
// never seen is the capability question the suite exists to ask, not a fact a
// unit test may pin.
func aPositiveScenario() AuthoredScenario {
	return AuthoredScenario{
		ID:          "adv-soc2-letter",
		Kind:        "commitment-slip",
		Description: "Avery promised the SOC 2 gap letter by Friday and the thread has gone quiet since.",
		MustSurface: true,
		Expect:      expect("commitments", "SOC 2 gap letter"),
		Emails: []AuthoredEmail{
			{
				ID:        "adv-e-soc2-1",
				ThreadID:  "adv-t-soc2",
				DayOffset: -6,
				Hour:      11,
				Minute:    2,
				From:      person("Priya Raman", "priya@halberd.example"),
				To:        []model.Person{person("Avery Chen", "avery@tessera.io")},
				Subject:   "SOC 2 gap letter for the security review",
				Body:      "Avery — our security team needs the SOC 2 gap letter before they can sign off. Any chance you can get it over this week?",
			},
			{
				ID:        "adv-e-soc2-2",
				ThreadID:  "adv-t-soc2",
				DayOffset: -5,
				Hour:      8,
				Minute:    41,
				From:      person("Avery Chen", "avery@tessera.io"),
				To:        []model.Person{person("Priya Raman", "priya@halberd.example")},
				Subject:   "Re: SOC 2 gap letter for the security review",
				Body:      "yes — I'll send you the SOC 2 gap letter by Friday.",
				InReplyTo: "adv-e-soc2-1",
			},
		},
		Tasks: []AuthoredTask{{
			ID:           "adv-task-soc2",
			Title:        "Send Halberd the SOC 2 gap letter",
			DueDayOffset: intPtr(-2),
			Owner:        "Avery",
		}},
	}
}

// scripted renders scenarios as the model's schema-shaped reply.
func scripted(t *testing.T, scenarios ...AuthoredScenario) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"scenarios": scenarios})
	if err != nil {
		t.Fatalf("cannot encode the scripted answer: %v", err)
	}
	return string(raw)
}

// runAdversarial drives the suite over the pinned corpus with a scripted author.
func runAdversarial(t *testing.T, ref reference, answers ...string) (*Adversarial, *runnertest.Runner) {
	t.Helper()
	author := &runnertest.Runner{RunnerName: "scripted", Answers: answers}
	a := RunAdversarial(context.Background(), AdversarialInput{
		Author:  author,
		DataDir: ref.Dir,
		WorkDir: t.TempDir(),
		Corpus:  ref.Corpus,
		Traps:   ref.Traps,
		Today:   ref.Day,
		Loc:     ref.Loc,
		Want:    2,
	})
	return a, author
}

// The promise the whole suite rests on: the dataset it was pointed at is the
// dataset it leaves behind, to the byte.
func TestAdversarialLeavesTheOriginalCorpusByteIdentical(t *testing.T) {
	ref := buildReference(t)
	before := fingerprint(t, ref.Dir)

	a, _ := runAdversarial(t, ref, scripted(t, aNegativeScenario(), aPositiveScenario()))
	if a.Err != nil {
		t.Fatalf("adversarial run: %v", a.Err)
	}
	if len(a.Traps) != 2 {
		t.Fatalf("injected %d traps, want 2", len(a.Traps))
	}

	after := fingerprint(t, ref.Dir)
	if len(before) != len(after) {
		t.Fatalf("the original corpus gained or lost files: %d before, %d after", len(before), len(after))
	}
	for path, sum := range before {
		got, ok := after[path]
		if !ok {
			t.Errorf("%s disappeared from the original corpus", path)
			continue
		}
		if got != sum {
			t.Errorf("%s was rewritten: %s -> %s", path, sum, got)
		}
	}

	// And the copy really did receive the injection, or the assertion above is
	// satisfied by a suite that did nothing.
	copied := fingerprint(t, a.Dir)
	if copied["inbox.jsonl"] == before["inbox.jsonl"] {
		t.Error("the copy's inbox is identical to the original; nothing was injected")
	}
	if copied["traps.json"] == before["traps.json"] {
		t.Error("the copy's answer key is identical to the original; nothing was injected")
	}
}

// The injected copy is a corpus the engine can read and the harness can grade,
// and the planted exam still stands inside it.
func TestAdversarialGradesTheInjectedCopy(t *testing.T) {
	ref := buildReference(t)

	a, _ := runAdversarial(t, ref, scripted(t, aNegativeScenario(), aPositiveScenario()))
	if a.Err != nil {
		t.Fatalf("adversarial run: %v", a.Err)
	}
	if a.Result == nil || len(a.Result.Traps) != 2 {
		t.Fatalf("the authored tasks were not graded: %+v", a.Result)
	}

	// A negative task is the one verdict a unit test may pin: an engine that
	// surfaces an unsolicited vendor blast has a suppression bug, not a taste.
	got, ok := a.Result.Get("adv-vendor-blast")
	if !ok {
		t.Fatal("the authored negative task was not graded")
	}
	if !got.Passed {
		t.Errorf("the vendor blast surfaced in the digest: %s", got.Reason)
	}

	// The control: injecting new questions must not knock over the planted ones,
	// or every number above is about the injection rather than the engine.
	if a.Control == nil || !a.Control.Passed() {
		t.Errorf("the planted exam broke over the injected corpus: %v", a.Control.Failures())
	}

	for _, name := range []string{"digest.md", "signals.json", AuthoredFile} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(a.Dir), name)); err != nil {
			t.Errorf("the run left no %s behind: %v", name, err)
		}
	}
}

// A scenario that breaks the contract is rejected with its reasons and re-asked
// exactly once — and the accepted half of a mixed answer is kept rather than
// thrown away with it.
func TestAdversarialRejectsAnInvalidScenarioAndRetriesOnce(t *testing.T) {
	ref := buildReference(t)

	bad := aPositiveScenario()
	bad.ID = "commitment-cap-table-slip" // already in the answer key
	bad.Kind = "not-a-category"
	bad.Expect = expect("commitments", "a phrase this scenario never plants")

	a, author := runAdversarial(t, ref,
		scripted(t, aNegativeScenario(), bad),
		scripted(t, aPositiveScenario()),
	)
	if a.Err != nil {
		t.Fatalf("adversarial run: %v", a.Err)
	}
	if author.Asks() != 2 {
		t.Errorf("the author was asked %d times; an invalid answer buys exactly one retry", author.Asks())
	}
	if a.Attempts != 2 {
		t.Errorf("Attempts is %d, want 2", a.Attempts)
	}
	if len(a.Rejections) != 1 {
		t.Fatalf("recorded %d rejections, want 1: %+v", len(a.Rejections), a.Rejections)
	}

	reason := a.Rejections[0].Reason
	for _, want := range []string{"already taken", "not a trap category", "appears nowhere in the text"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the rejection does not say %q, so the retry was not told what to fix:\n%s", want, reason)
		}
	}
	if ids := trapIDs(a); ids != "adv-soc2-letter,adv-vendor-blast" {
		t.Errorf("kept %s; the valid half of the first answer and the replacement should both survive", ids)
	}
}

// One retry and no more. A model that cannot use a specific correction will not
// be helped by a third attempt, and an authoring loop with no ceiling is a
// harness with an unbounded bill.
func TestAdversarialStopsAfterOneRetry(t *testing.T) {
	ref := buildReference(t)

	bad := aPositiveScenario()
	bad.Expect = expect("commitments", "a phrase this scenario never plants")

	a, author := runAdversarial(t, ref, scripted(t, bad), scripted(t, bad))
	if author.Asks() != 2 {
		t.Errorf("the author was asked %d times, want 2", author.Asks())
	}
	if a.Err == nil {
		t.Fatal("a run that authored nothing must say so")
	}
	if len(a.Traps) != 0 || a.Dir != "" {
		t.Errorf("nothing may be injected when nothing validated (traps=%d dir=%q)", len(a.Traps), a.Dir)
	}
	if a.Skipped {
		t.Error("this is a failed run, not a skipped one; the two read differently on the card")
	}
}

// An off-schema answer is a failure to answer, not a hard trap.
func TestAdversarialRejectsAnAnswerThatIsNotTheSchema(t *testing.T) {
	ref := buildReference(t)

	a, _ := runAdversarial(t, ref, `{"scenarios":"three of them"}`, `{"scenarios":[]}`)
	if a.Err == nil {
		t.Fatal("an answer that is not the schema must not produce a grade")
	}
	if len(a.Rejections) == 0 {
		t.Fatal("the off-schema answer was not recorded as a rejection")
	}
	if !strings.Contains(a.Rejections[0].Reason, "does not match the scenario schema") {
		t.Errorf("the rejection does not name the schema: %s", a.Rejections[0].Reason)
	}
}

// A runner that will not answer is a failed run with the transport error on the
// card, not a crash and not a silent zero.
func TestAdversarialRecordsARunnerThatWouldNotAnswer(t *testing.T) {
	ref := buildReference(t)

	author := &runnertest.Runner{RunnerName: "scripted", AskErr: errors.New("401 unauthorized")}
	a := RunAdversarial(context.Background(), AdversarialInput{
		Author: author, DataDir: ref.Dir, WorkDir: t.TempDir(),
		Corpus: ref.Corpus, Traps: ref.Traps, Today: ref.Day, Loc: ref.Loc, Want: 1,
	})
	if a.Err == nil {
		t.Fatal("a runner that never answered must not read as a completed run")
	}
	// Prompts rather than Asks: the scripted runner only counts an Ask it
	// answered, and this one answered none.
	if n := len(author.Prompts()); n != 1 {
		t.Errorf("asked %d times; a runner that cannot answer is not worth re-asking", n)
	}
	if len(a.Rejections) != 1 || !strings.Contains(a.Rejections[0].Reason, "401") {
		t.Errorf("the transport failure is not on the card: %+v", a.Rejections)
	}
}

// No runner is a skip, and a skip is as loud as a failure.
func TestAdversarialSkipsLoudlyWithNoAuthor(t *testing.T) {
	ref := buildReference(t)

	a := RunAdversarial(context.Background(), AdversarialInput{
		DataDir: ref.Dir, WorkDir: t.TempDir(),
		Corpus: ref.Corpus, Traps: ref.Traps, Today: ref.Day, Loc: ref.Loc,
	})
	if !a.Skipped {
		t.Fatal("a run with no author must skip")
	}
	if a.Want != AdversarialScenarios {
		t.Errorf("Want is %d, want the default %d", a.Want, AdversarialScenarios)
	}

	card := (&Card{Regression: &Result{}, Adversarial: a}).Markdown()
	for _, want := range []string{"Adversarial suite", "SKIPPED", "This is a skip, not a pass"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card's skip is not loud enough; missing %q", want)
		}
	}
}

// planted_refs are a return value of the artifacts, never a field the model
// fills in — the same invariant datagen's Scenario signature enforces for the
// shipped catalog.
func TestCompileDerivesPlantedRefsFromTheArtifacts(t *testing.T) {
	ref := buildReference(t)
	ns := newNamespace(ref.Corpus, ref.Traps, ref.Day, ref.Loc)

	built, err := compile(aPositiveScenario(), ns)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var refs []string
	for _, r := range built.Trap.PlantedRefs {
		refs = append(refs, string(r.Source)+":"+r.Ref)
	}
	want := "email:adv-e-soc2-1,email:adv-e-soc2-2,task:adv-task-soc2"
	if got := strings.Join(refs, ","); got != want {
		t.Errorf("planted_refs = %s, want %s", got, want)
	}
	if err := built.Trap.Validate(); err != nil {
		t.Errorf("the compiled trap is not a valid answer-key entry: %v", err)
	}
}

// The rules that keep an authored scenario from grading nothing. Each case is
// one rule, and the assertion is on the reason rather than on the failure —
// a rejection the model cannot act on is as useless as no rejection.
func TestCompileRejectsScenariosThatWouldGradeNothing(t *testing.T) {
	ref := buildReference(t)

	cases := []struct {
		name   string
		mutate func(*AuthoredScenario)
		want   string
	}{
		{"unknown extractor", func(s *AuthoredScenario) { s.Expect.SignalKind = "vibes" }, "is not an extractor"},
		{"colliding email id", func(s *AuthoredScenario) {
			s.Emails[0].ID = ref.Corpus.Emails[0].ID
		}, "already exists in this corpus"},
		{"date outside the window", func(s *AuthoredScenario) { s.Emails[0].DayOffset = -400 }, "outside the corpus window"},
		{"mail dated in the future", func(s *AuthoredScenario) { s.Emails[0].DayOffset = 3 }, "outside the corpus window"},
		{"a reply that predates its parent", func(s *AuthoredScenario) { s.Emails[1].DayOffset = -9 }, "dated before the message it replies to"},
		{"a reply to nothing", func(s *AuthoredScenario) { s.Emails[1].InReplyTo = "e-does-not-exist" }, "not in this scenario"},
		{"no artifacts at all", func(s *AuthoredScenario) { s.Emails, s.Tasks = nil, nil }, "plants no artifacts"},
		{"an unplanted keyword", func(s *AuthoredScenario) {
			s.Expect = expect("commitments", "a phrase this scenario never plants")
		}, "appears nowhere in the text"},
		{"a sender with no address", func(s *AuthoredScenario) { s.Emails[0].From.Email = "" }, "email address is empty"},
		{"an id that is not a slug", func(s *AuthoredScenario) { s.ID = "Adversarial Trap #1" }, "must be a lowercase slug"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := aPositiveScenario()
			tc.mutate(&s)

			ns := newNamespace(ref.Corpus, ref.Traps, ref.Day, ref.Loc)
			if _, err := compile(s, ns); err == nil {
				t.Fatalf("compiled a scenario that %s", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the rejection does not say %q:\n%v", tc.want, err)
			}
		})
	}
}

// The schema is what constrains the model, so it has to be a schema.
func TestAdversarialSchemaIsValidJSONAndNamesTheClosedSets(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(adversarialSchema), &doc); err != nil {
		t.Fatalf("the authoring schema is not JSON: %v", err)
	}
	if strings.ContainsAny(adversarialSchema, "\n\r") {
		t.Error("the schema travels as one argv entry; it must carry no newlines")
	}
	for _, want := range []string{"commitment-slip", "quiet-threads", "must_surface", "day_offset", "planted_refs"} {
		has := strings.Contains(adversarialSchema, `"`+want+`"`)
		if want == "planted_refs" {
			if has {
				t.Error("the schema asks the model for planted_refs; they are derived from the artifacts, never authored")
			}
			continue
		}
		if !has {
			t.Errorf("the schema does not mention %q", want)
		}
	}
}

// trapIDs is the sorted id list, for an order-independent assertion.
func trapIDs(a *Adversarial) string {
	ids := make([]string, 0, len(a.Traps))
	for _, t := range a.Traps {
		ids = append(ids, t.ID)
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}

// fingerprint hashes every regular file under dir, keyed by its relative path.
func fingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("cannot fingerprint %s: %v", dir, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty; there is nothing to compare", dir)
	}
	return out
}
