package extract

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The property the whole eval rests on: same data plus same --today gives
// byte-identical output. Run twice, from two independently loaded corpora, and
// compare the serialized bytes — that catches map iteration order, which is the
// only realistic way for this to break and the one a spot-check would miss.
func TestAllIsDeterministic(t *testing.T) {
	encode := func() []byte {
		ss, err := loadFixture(t, "corpus", fixtureDay).All()
		if err != nil {
			t.Fatalf("All: %v", err)
		}
		data, err := json.Marshal(ss)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return data
	}

	first := encode()
	for i := 0; i < 5; i++ {
		if got := encode(); string(got) != string(first) {
			t.Fatalf("run %d differs from run 0\nfirst: %s\ngot:   %s", i+1, first, got)
		}
	}
}

// Every signal carries at least one citation, and every citation points at a
// record that exists. A signal with no receipt is a claim the architecture
// cannot back, and a citation pointing nowhere is worse than none.
func TestEverySignalIsCitedAndResolvable(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, err := tb.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(ss) == 0 {
		t.Fatal("the fixture corpus produced no signals at all")
	}

	emails := map[string]bool{}
	for _, e := range tb.corpus.Emails {
		emails[e.ID] = true
	}
	events := map[string]bool{}
	for _, e := range tb.corpus.Events {
		events[e.UID] = true
	}
	notes := map[string]bool{}
	for _, n := range tb.corpus.Notes {
		notes[n.Path] = true
	}
	tasks := map[string]bool{}
	for _, task := range tb.corpus.Tasks {
		tasks[task.ID] = true
	}

	for _, s := range ss {
		if len(s.Citations) == 0 {
			t.Errorf("%s: no citations", s.ID)
			continue
		}
		for _, c := range s.Citations {
			var known bool
			switch c.Source {
			case model.SourceEmail:
				known = emails[c.Ref]
			case model.SourceCalendar:
				known = events[c.Ref]
			case model.SourceNote:
				known = notes[c.Ref]
			case model.SourceTask:
				known = tasks[c.Ref]
			}
			if !known {
				t.Errorf("%s cites %s:%s, which is not in the corpus", s.ID, c.Source, c.Ref)
			}
		}
	}
}

// Signal ids are derived from the records they are about, not from a counter,
// so a trap can be written against one. They must also be unique — the eval
// harness indexes by id.
func TestSignalIDsAreStableAndUnique(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	seen := map[string]bool{}
	for _, s := range ss {
		if seen[s.ID] {
			t.Errorf("duplicate signal id %q", s.ID)
		}
		seen[s.ID] = true
		if !strings.HasPrefix(s.ID, s.Kind+":") {
			t.Errorf("signal id %q does not start with its kind %q", s.ID, s.Kind)
		}
	}
}

// The reading order is priority, then deadline, then extractor, then id — and
// it ends on a unique key, so there is exactly one correct output.
func TestSignalsAreSortedIntoReadingOrder(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).All()

	for i := 1; i < len(ss); i++ {
		a, b := ss[i-1], ss[i]
		if a.Priority.Rank() > b.Priority.Rank() {
			t.Fatalf("out of order at %d: %s (%s) before %s (%s)", i, a.ID, a.Priority, b.ID, b.Priority)
		}
		if a.Priority != b.Priority {
			continue
		}
		if ad, bd := deadlineKey(a), deadlineKey(b); ad.After(bd) {
			t.Fatalf("deadlines out of order at %d: %s then %s", i, ad, bd)
		}
	}
}

// Every published extractor runs, and every kind it produces is one the model
// knows about — traps.json, `aubade tool` and signals.json speak one dialect.
func TestAllRunsEveryExtractor(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, _ := tb.All()

	produced := byKind(ss)
	for _, kind := range Kinds() {
		if !model.IsKnownKind(kind) {
			t.Errorf("extractor %q is not in model.KnownKinds", kind)
		}
		if _, ran := produced[kind]; !ran {
			// Not every extractor must fire on every corpus, but this fixture
			// is built so that all seven do.
			t.Errorf("extractor %q produced nothing on the fixture corpus", kind)
		}
	}
}

// The tool dispatch surface: every published name resolves, thread and search
// return their own payloads, and a guessed name gets the menu back.
func TestRunDispatchesTheWholeToolbox(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	for _, name := range Kinds() {
		res, err := tb.Run(name, "")
		if err != nil {
			t.Errorf("Run(%q): %v", name, err)
			continue
		}
		if res.Thread != nil || res.Search != nil {
			t.Errorf("Run(%q) returned an investigation payload", name)
		}
	}

	res, err := tb.Run("thread", "t-capt")
	if err != nil || res.Thread == nil {
		t.Errorf("Run(thread): %v / %+v", err, res)
	}
	res, err = tb.Run("search", "cap table")
	if err != nil || res.Search == nil {
		t.Errorf("Run(search): %v / %+v", err, res)
	}

	_, err = tb.Run("committments", "")
	if err == nil || !strings.Contains(err.Error(), "quiet-threads") {
		t.Errorf("a misspelled tool should list the alternatives, got: %v", err)
	}
}

// ToolNames is the agent's menu: the seven extractors plus the two
// investigation tools, in that order.
func TestToolNames(t *testing.T) {
	names := ToolNames()
	if len(names) != len(model.KnownKinds)+2 {
		t.Fatalf("ToolNames() = %v", names)
	}
	if names[len(names)-2] != "thread" || names[len(names)-1] != "search" {
		t.Errorf("investigation tools should come last: %v", names)
	}
}

// signals.json is written by one function and read by another so the writer and
// the eval harness cannot drift into disagreeing about the shape.
func TestWriteAndReadSignalsRoundTrip(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	path := filepath.Join(t.TempDir(), "nested", SignalsFile)
	if err := WriteSignals(path, ss); err != nil {
		t.Fatalf("WriteSignals: %v", err)
	}
	got, err := ReadSignals(path)
	if err != nil {
		t.Fatalf("ReadSignals: %v", err)
	}
	if len(got) != len(ss) {
		t.Fatalf("round trip changed the count: %d → %d", len(ss), len(got))
	}
	for i := range ss {
		if got[i].ID != ss[i].ID || got[i].Title != ss[i].Title {
			t.Fatalf("round trip changed signal %d: %+v vs %+v", i, ss[i], got[i])
		}
	}
}

// An uncitable signal must never reach a file the digest is composed from.
func TestWriteSignalsRefusesAnInvalidSet(t *testing.T) {
	bad := model.Signals{{
		ID: "x", Kind: "commitments", Priority: model.P0, Title: "t",
		SectionHint: model.SectionUrgentToday, Confidence: model.Certain,
	}}
	path := filepath.Join(t.TempDir(), SignalsFile)
	if err := WriteSignals(path, bad); err == nil {
		t.Fatal("WriteSignals accepted a signal with no citations")
	}
}

// The anchor day is required, and it is a date, not a guess.
func TestParseTodayAndNewValidateTheirInputs(t *testing.T) {
	loc := model.Location()
	if _, err := ParseToday("", loc); err == nil {
		t.Error("an empty --today should be an error, not a silent fall back to the clock")
	}
	if _, err := ParseToday("31/08/2026", loc); err == nil {
		t.Error("a non-ISO date should be rejected")
	}
	day, err := ParseToday("2026-08-31", loc)
	if err != nil {
		t.Fatalf("ParseToday: %v", err)
	}

	if _, err := New(nil, day, loc); err == nil {
		t.Error("New(nil corpus) should be an error")
	}
	if _, err := New(&model.Corpus{}, day, nil); err != nil {
		t.Errorf("a nil location should default to the corpus zone: %v", err)
	}
}

// The anchor day is an input, so moving it moves the answers. Overdue on Monday
// is not overdue on the Thursday it was promised.
func TestTodayChangesTheAnswers(t *testing.T) {
	onThursday, err := loadFixture(t, "corpus", "2026-08-27").Commitments()
	if err != nil {
		t.Fatalf("Commitments: %v", err)
	}
	if _, found := findSignal(onThursday, "commitments:e-002"); found {
		t.Error("a promise made at 18:10 is not overdue at 06:00 that morning")
	}

	onMonday, _ := loadFixture(t, "corpus", fixtureDay).Commitments()
	if _, found := findSignal(onMonday, "commitments:e-002"); !found {
		t.Error("the same promise is overdue four days later")
	}
}

// With no profile at all the toolbox still runs: priorities default, nothing is
// suppressed, and the staleness extractor says the profile was missing.
func TestToolboxSurvivesAMissingProfile(t *testing.T) {
	tb := loadFixture(t, "thin", fixtureDay)
	ss, err := tb.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if err := ss.Validate(); err != nil {
		t.Errorf("signals from a profile-less corpus do not validate: %v", err)
	}
	if _, found := findSignal(ss, "staleness:missing:profile"); !found {
		t.Error("a missing profile must be reported, not silently defaulted")
	}
	if tb.Owner().Email == "" {
		t.Error("the owner should still be derivable from the mail")
	}
}
