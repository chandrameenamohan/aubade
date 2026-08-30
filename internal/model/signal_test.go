package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validSignal() Signal {
	return Signal{
		ID:          "sig-commitments-001",
		Kind:        KindCommitments,
		Priority:    P0,
		Title:       "Send Marcus the updated cap table",
		Detail:      "Promised Tuesday night; still not sent.",
		Citations:   []Citation{{Source: SourceEmail, Ref: "e-002"}},
		SectionHint: SectionOneThingNow,
		Confidence:  Certain,
	}
}

func TestSignalValidate(t *testing.T) {
	if err := validSignal().Validate(); err != nil {
		t.Fatalf("valid signal rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Signal)
		want   string
	}{
		{"no id", func(s *Signal) { s.ID = "" }, `"id" is empty`},
		{"no kind", func(s *Signal) { s.Kind = " " }, `"kind" is empty`},
		{"bad priority", func(s *Signal) { s.Priority = "P9" }, "invalid priority"},
		{"no title", func(s *Signal) { s.Title = "" }, `"title" is empty`},
		{"no section", func(s *Signal) { s.SectionHint = "" }, `"section_hint" is empty`},
		{"bad confidence", func(s *Signal) { s.Confidence = "maybe" }, "invalid confidence"},
		// The load-bearing one: a signal with no receipt cannot enter a digest.
		{"no citations", func(s *Signal) { s.Citations = nil }, "no citations"},
		{"bad citation source", func(s *Signal) { s.Citations[0].Source = "slack" }, "citation[0] source"},
		{"empty citation ref", func(s *Signal) { s.Citations[0].Ref = " " }, "empty ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSignal()
			tc.mutate(&s)
			err := s.Validate()
			if err == nil {
				t.Fatalf("accepted an invalid signal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The eval harness indexes signals by id, so two signals sharing one would make
// a trap silently ungradeable.
func TestSignalsRejectDuplicateIDs(t *testing.T) {
	ss := Signals{validSignal(), validSignal()}
	err := ss.Validate()
	if err == nil {
		t.Fatalf("accepted duplicate signal ids")
	}
	if !strings.Contains(err.Error(), "duplicate signal id") {
		t.Errorf("error = %q, want it to name the duplicate", err)
	}
}

func TestSignalsValidateReportsIndex(t *testing.T) {
	bad := validSignal()
	bad.ID = "sig-2"
	bad.Citations = nil
	err := Signals{validSignal(), bad}.Validate()
	if err == nil || !strings.Contains(err.Error(), "signals[1]") {
		t.Fatalf("error = %v, want it to name the offending index", err)
	}
}

// signals.json is a contract the eval harness and every agent caller bind to.
func TestSignalJSONContract(t *testing.T) {
	s := validSignal()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "kind", "priority", "title", "detail", "citations", "section_hint", "confidence"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("marshalled signal has no %q key: %s", key, raw)
		}
	}
	if _, ok := generic["deadline"]; ok {
		t.Errorf("absent deadline was marshalled: %s", raw)
	}

	cites, ok := generic["citations"].([]any)
	if !ok || len(cites) != 1 {
		t.Fatalf("citations = %v, want one entry", generic["citations"])
	}
	cite, ok := cites[0].(map[string]any)
	if !ok || cite["source"] != "email" || cite["ref"] != "e-002" {
		t.Errorf("citation = %v, want {source: email, ref: e-002}", cites[0])
	}

	deadline := time.Date(2026, 8, 30, 11, 0, 0, 0, time.UTC)
	s.Deadline = &deadline
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal with deadline: %v", err)
	}
	if !strings.Contains(string(raw), `"deadline":"2026-08-30T11:00:00Z"`) {
		t.Errorf("deadline not marshalled as RFC3339: %s", raw)
	}
}

func TestPriority(t *testing.T) {
	if P0.Rank() != 0 || P4.Rank() != 4 {
		t.Errorf("ranks = %d, %d; want 0, 4", P0.Rank(), P4.Rank())
	}
	if Priority("P5").Rank() != -1 || Priority("P5").Valid() {
		t.Errorf("P5 accepted as a priority")
	}

	for _, in := range []string{"P0", "p0", " P0 "} {
		got, err := ParsePriority(in)
		if err != nil || got != P0 {
			t.Errorf("ParsePriority(%q) = %v, %v; want P0", in, got, err)
		}
	}
	for _, in := range []string{"", "high", "P5", "0"} {
		if _, err := ParsePriority(in); err == nil {
			t.Errorf("ParsePriority(%q) accepted a priority it should reject", in)
		}
	}
}

// The signal vocabulary is deliberately the toolbox's extractor names, so
// traps.json, `aubade tool <name>` and signals.json cannot drift apart.
func TestKindAndSectionVocabulary(t *testing.T) {
	for _, kind := range KnownKinds {
		if !IsKnownKind(kind) {
			t.Errorf("IsKnownKind(%q) = false for a published kind", kind)
		}
	}
	if IsKnownKind("thread") {
		t.Errorf("thread is an investigation tool, not a signal kind")
	}
	if len(KnownKinds) != 7 {
		t.Errorf("KnownKinds has %d entries, want the 7 signal-emitting extractors", len(KnownKinds))
	}

	for _, hint := range KnownSectionHints {
		if !IsKnownSectionHint(hint) {
			t.Errorf("IsKnownSectionHint(%q) = false for a published section", hint)
		}
	}
	if IsKnownSectionHint("appendix") {
		t.Errorf("an unpublished section hint was accepted")
	}
	if KnownSectionHints[0] != SectionOneThingNow {
		t.Errorf("section order starts at %q, want the one-thing-now section", KnownSectionHints[0])
	}
}

func TestConfidenceAndSourceValidity(t *testing.T) {
	if !Certain.Valid() || !Unsure.Valid() || Confidence("probably").Valid() {
		t.Errorf("confidence validity is wrong")
	}
	for _, s := range []Source{SourceEmail, SourceCalendar, SourceNote, SourceTask} {
		if !s.Valid() {
			t.Errorf("Source(%q) reported invalid", s)
		}
	}
	if Source("profile").Valid() {
		t.Errorf("profile is not a citable source: citations point at data, not at settings")
	}
}
