package extract

import (
	"strings"
	"testing"
)

// Word boundaries are the whole reason the lexicons are safe to use. Substring
// matching would let "sent" fire on "absent" and "on it" on "onion", which is
// exactly the class of false positive an honesty layer cannot afford.
func TestContainsWordRespectsBoundaries(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             bool
	}{
		{"I'll send it tonight", "i'll", true},
		{"I’ll send it tonight", "i'll", true}, // curly apostrophe
		{"the meeting was absent a chair", "sent", false},
		{"we sent it", "sent", true},
		{"onion soup", "on it", false},
		{"I'm on it", "on it", true},
		{"Stratechery", "strat", false},
		{"café tomorrow", "tomorrow", true},
	}
	for _, tc := range cases {
		if got := containsWord(tc.haystack, tc.needle); got != tc.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

// Distinctive tokens are what tie an email to a meeting and a promise to a
// task. Short words and stopwords are dropped because they identify nothing.
func TestDistinctiveTokens(t *testing.T) {
	got := distinctiveTokens("Re: the updated cap table before diligence")
	for _, unwanted := range []string{"the", "cap", "before"} {
		if contains(got, unwanted) {
			t.Errorf("distinctiveTokens kept %q: %v", unwanted, got)
		}
	}
	for _, wanted := range []string{"updated", "table", "diligence"} {
		if !contains(got, wanted) {
			t.Errorf("distinctiveTokens dropped %q: %v", wanted, got)
		}
	}

	// Stemming is one trailing "s", so plurals match singulars.
	if !contains(distinctiveTokens("newsletters"), "newsletter") {
		t.Error("newsletters should stem to newsletter")
	}
	// …and not two, so "address" survives intact.
	if !contains(distinctiveTokens("address"), "address") {
		t.Error("address should not be stemmed to addres")
	}
}

// Sentence scoping is what keeps a promise and an unrelated date from fusing
// into a commitment nobody made.
func TestSentencesSplitOnPunctuationAndNewlines(t *testing.T) {
	got := sentences("I'll send the deck. Separately, the board meeting is Thursday!\nOne more thing")
	if len(got) != 3 {
		t.Fatalf("sentences() = %q, want 3", got)
	}
	if got[0] != "I'll send the deck" {
		t.Errorf("first sentence = %q", got[0])
	}
}

// asksSomething decides whose turn it is, which every thread-shaped extractor
// depends on.
func TestAsksSomething(t *testing.T) {
	cases := map[string]bool{
		"Can you send the deck?":            true,
		"Let me know by Friday":             true,
		"Need a yes or no":                  true,
		"Thanks, got it":                    false,
		"FYI, QA opened two regressions":    false,
		"looked at it. numbers hold up":     false,
		"is the Sept 4 rollout still on?":   true,
		"ping me if anything is missing":    false,
		"Our stakeholder meeting is at ten": false,
	}
	for text, want := range cases {
		if got := asksSomething(text); got != want {
			t.Errorf("asksSomething(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestTruncateCutsOnAWordBoundary(t *testing.T) {
	got := truncate("the quick brown fox jumps over the lazy dog", 20)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate() = %q, want an ellipsis", got)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Errorf("truncate() left a trailing space: %q", got)
	}
	if unchanged := truncate("short", 20); unchanged != "short" {
		t.Errorf("truncate() = %q, want the input unchanged", unchanged)
	}
}

func TestNormalizeSubjectStripsReplyPrefixes(t *testing.T) {
	cases := map[string]string{
		"Re: cap table":      "cap table",
		"RE: FWD: cap table": "cap table",
		"Fwd: Re: cap table": "cap table",
		"cap table":          "cap table",
	}
	for in, want := range cases {
		if got := normalizeSubject(in); got != want {
			t.Errorf("normalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
