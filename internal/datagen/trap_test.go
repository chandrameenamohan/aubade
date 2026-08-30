package datagen

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func sampleTrap() Trap {
	return Trap{
		ID:          "commitment-cap-table-slip",
		Kind:        CommitmentSlip,
		Description: "Avery promised the cap table twice and sent it neither time.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindCommitments,
			Keywords:   []string{"cap table"},
		},
		PlantedRefs: []model.Citation{{Source: model.SourceEmail, Ref: "m-capt-02"}},
	}
}

// The traps.json shape is the contract the eval harness binds to (SPEC
// "Binding contracts"). Asserting the exact key set in both directions is the
// point: a field quietly added here is a field the harness will not read, and a
// field quietly renamed is a harness that reads zero values and grades nothing.
func TestTrapsJSONSchema(t *testing.T) {
	raw, err := json.Marshal(Traps{sampleTrap()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("traps.json is not an array of objects: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d traps, want 1", len(decoded))
	}

	assertKeys(t, "trap", decoded[0], "id", "kind", "description", "must_surface", "expect", "planted_refs")
	assertString(t, "id", decoded[0]["id"])
	assertString(t, "kind", decoded[0]["kind"])
	assertString(t, "description", decoded[0]["description"])
	if _, ok := decoded[0]["must_surface"].(bool); !ok {
		t.Errorf("must_surface is %T, want bool", decoded[0]["must_surface"])
	}

	expect, ok := decoded[0]["expect"].(map[string]any)
	if !ok {
		t.Fatalf("expect is %T, want an object", decoded[0]["expect"])
	}
	assertKeys(t, "expect", expect, "signal_kind", "keywords")
	assertString(t, "expect.signal_kind", expect["signal_kind"])
	keywords, ok := expect["keywords"].([]any)
	if !ok || len(keywords) == 0 {
		t.Fatalf("expect.keywords is %T, want a non-empty array", expect["keywords"])
	}
	assertString(t, "expect.keywords[0]", keywords[0])

	refs, ok := decoded[0]["planted_refs"].([]any)
	if !ok || len(refs) == 0 {
		t.Fatalf("planted_refs is %T, want a non-empty array", decoded[0]["planted_refs"])
	}
	ref, ok := refs[0].(map[string]any)
	if !ok {
		t.Fatalf("planted_refs[0] is %T, want an object", refs[0])
	}
	assertKeys(t, "planted_refs[0]", ref, "source", "ref")
	assertString(t, "planted_refs[0].source", ref["source"])
	assertString(t, "planted_refs[0].ref", ref["ref"])
}

// The whole generated key has to satisfy the same schema, not just a fixture of
// it: this is what actually gets written to traps.json.
func TestGeneratedTrapsSchemaValidate(t *testing.T) {
	plan := mustBuild(t)
	if err := plan.Traps.Validate(); err != nil {
		t.Fatalf("generated answer key is invalid: %v", err)
	}

	var buf bytes.Buffer
	if err := EncodeTraps(&buf, plan.Traps); err != nil {
		t.Fatalf("EncodeTraps: %v", err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Error("traps.json does not end in a newline")
	}

	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("written traps.json does not parse: %v", err)
	}
	if len(decoded) != len(plan.Traps) {
		t.Fatalf("wrote %d traps, generated %d", len(decoded), len(plan.Traps))
	}
	for i, obj := range decoded {
		assertKeys(t, plan.Traps[i].ID, obj, "id", "kind", "description", "must_surface", "expect", "planted_refs")
	}

	var round Traps
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if !reflect.DeepEqual(round, plan.Traps) {
		t.Error("traps.json does not round-trip back to the key that wrote it")
	}
}

// EncodeTraps must be byte-stable and must refuse to write a key it knows is
// broken: a traps.json nobody can trust is worse than no traps.json, because
// the harness will happily grade against it.
func TestEncodeTrapsIsStableAndRefusesInvalid(t *testing.T) {
	traps := Traps{sampleTrap()}
	var first, second bytes.Buffer
	if err := EncodeTraps(&first, traps); err != nil {
		t.Fatalf("EncodeTraps: %v", err)
	}
	if err := EncodeTraps(&second, traps); err != nil {
		t.Fatalf("EncodeTraps: %v", err)
	}
	if first.String() != second.String() {
		t.Error("EncodeTraps is not byte-stable")
	}

	broken := sampleTrap()
	broken.Expect.Keywords = nil
	var out bytes.Buffer
	if err := EncodeTraps(&out, Traps{broken}); err == nil {
		t.Error("EncodeTraps wrote an answer key with no keywords")
	}
	if out.Len() != 0 {
		t.Error("EncodeTraps wrote bytes for a key it rejected")
	}
}

func TestTrapValidate(t *testing.T) {
	if err := sampleTrap().Validate(); err != nil {
		t.Fatalf("valid trap rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Trap)
		want   string
	}{
		{"no id", func(tr *Trap) { tr.ID = " " }, `"id" is empty`},
		{"unknown category", func(tr *Trap) { tr.Kind = "vibes" }, "not a trap category"},
		{"extractor as category", func(tr *Trap) { tr.Kind = model.KindCommitments }, "not a trap category"},
		{"no description", func(tr *Trap) { tr.Description = "" }, "description is empty"},
		{"unknown extractor", func(tr *Trap) { tr.Expect.SignalKind = "hunches" }, "not an extractor"},
		{"category as extractor", func(tr *Trap) { tr.Expect.SignalKind = CommitmentSlip }, "not an extractor"},
		{"no keywords", func(tr *Trap) { tr.Expect.Keywords = nil }, "keywords is empty"},
		{"blank keyword", func(tr *Trap) { tr.Expect.Keywords = []string{" "} }, "is blank"},
		{"no refs", func(tr *Trap) { tr.PlantedRefs = nil }, "no planted_refs"},
		{"bad ref source", func(tr *Trap) { tr.PlantedRefs[0].Source = "inbox" }, "planted_refs[0] source"},
		{"empty ref", func(tr *Trap) { tr.PlantedRefs[0].Ref = "" }, "empty ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := sampleTrap()
			tr.PlantedRefs = append([]model.Citation(nil), tr.PlantedRefs...)
			tr.Expect.Keywords = append([]string(nil), tr.Expect.Keywords...)
			tc.mutate(&tr)
			err := tr.Validate()
			if err == nil {
				t.Fatal("accepted an invalid trap")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestTrapsValidateRejectsDuplicateIDs(t *testing.T) {
	err := Traps{sampleTrap(), sampleTrap()}.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate trap id") {
		t.Errorf("error = %v, want a duplicate id complaint", err)
	}
}

func TestPositiveNegativeSplit(t *testing.T) {
	positive, negative := sampleTrap(), sampleTrap()
	negative.ID = "negative-newsletter-stratechery"
	negative.Kind = Newsletter
	negative.MustSurface = false
	traps := Traps{positive, negative}

	if got := len(traps.Positive()); got != 1 {
		t.Errorf("Positive() = %d traps, want 1", got)
	}
	if got := traps.Negative(); len(got) != 1 || got[0].ID != negative.ID {
		t.Errorf("Negative() = %v, want just the newsletter", got)
	}
}

// SignalKinds reports in the canonical extractor order rather than the order
// traps happen to be listed in, so a scorecard's columns do not move when a
// scenario is added.
func TestSignalKindsIsCanonicallyOrdered(t *testing.T) {
	first, second := sampleTrap(), sampleTrap()
	second.ID = "conflict-double-booked"
	second.Kind = CalendarOverlap
	second.Expect.SignalKind = model.KindConflicts

	got := Traps{second, first}.SignalKinds()
	want := []string{model.KindCommitments, model.KindConflicts}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SignalKinds() = %v, want %v", got, want)
	}
}

func assertKeys(t *testing.T, what string, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(got, sorted) {
		t.Errorf("%s keys = %v, want exactly %v", what, got, sorted)
	}
}

func assertString(t *testing.T, what string, v any) {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s is %T, want string", what, v)
		return
	}
	if strings.TrimSpace(s) == "" {
		t.Errorf("%s is empty", what)
	}
}
