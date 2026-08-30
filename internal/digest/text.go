package digest

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Small text helpers for composition. Everything here is lexical and boring on
// purpose: the page has to be explainable line by line, and a reviewer must be
// able to say why a word ended up where it did.

// wordRE splits text into lowercase alphanumeric words.
var wordRE = regexp.MustCompile(`[^a-z0-9]+`)

// containsAny reports whether s contains any of the given substrings. s is
// expected to be lowercased by the caller — the callers all lowercase once and
// test many needles, and lowercasing per needle is the kind of waste that shows
// up in nothing but a profile.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// containsWholeWord reports whether haystack contains needle with a boundary on
// both ends. Substring matching is how "sam" starts matching "samantha" and
// somebody's draft goes to the wrong register.
func containsWholeWord(haystack, needle string) bool {
	h, n := strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return false
	}
	for i := 0; i < len(h); {
		j := strings.Index(h[i:], n)
		if j < 0 {
			return false
		}
		j += i
		if boundaryBefore(h, j) && boundaryAfter(h, j+len(n)) {
			return true
		}
		i = j + 1
	}
	return false
}

func boundaryBefore(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// stopwords are the words too common to identify an audience. Short list: it
// exists so "the" and "during" do not match a contact, not to do linguistics.
var stopwords = map[string]bool{
	"and": true, "are": true, "any": true, "anything": true, "the": true,
	"during": true, "from": true, "for": true, "with": true, "who": true,
	"that": true, "this": true, "them": true, "they": true, "more": true,
	"still": true, "just": true, "into": true, "over": true, "raise": true,
	"cold": true, "emailing": true, "me": true, "my": true, "our": true,
}

// distinctiveWords are the words in a phrase that can plausibly identify
// somebody: three letters or more, not a stopword, deduplicated in order.
func distinctiveWords(s string) []string {
	var out []string
	for _, w := range wordRE.Split(strings.ToLower(s), -1) {
		if len(w) < 3 || stopwords[w] {
			continue
		}
		if !contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// appendUnique appends values not already present, preserving order.
func appendUnique(dst []string, values ...string) []string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// quoted wraps a fragment of somebody else's words.
func quoted(s string) string { return `"` + s + `"` }

// clipWords shortens s to at most n words, marking the cut.
func clipWords(s string, n int) string {
	fields := strings.Fields(s)
	if n <= 0 || len(fields) <= n {
		return strings.Join(fields, " ")
	}
	return strings.Join(fields[:n], " ") + "…"
}

// capitalize upper-cases the first rune of s.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// sentenceEnd gives a lead sentence its full stop, unless it already ends in
// punctuation that does the same job.
func sentenceEnd(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.ContainsRune(".!?:;", rune(s[len(s)-1])) {
		return s
	}
	return s + "."
}

// firstQuestion returns the first sentence of a body that ends in a question
// mark — the line a one-reply answer would be answering.
//
// Deliberately narrower than the toolbox's ask detection: this text goes into a
// draft the user may send, so it quotes a question somebody literally asked
// rather than a sentence that merely reads like an ask.
func firstQuestion(body string) string {
	for _, chunk := range strings.Split(body, "\n") {
		for _, sentence := range splitSentences(chunk) {
			if strings.HasSuffix(sentence, "?") {
				return sentence
			}
		}
	}
	return ""
}

var sentenceSplitRE = regexp.MustCompile(`[^.!?]+[.!?]?`)

// splitSentences breaks a line into trimmed sentences, keeping terminators.
func splitSentences(line string) []string {
	var out []string
	for _, s := range sentenceSplitRE.FindAllString(line, -1) {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// wordCount counts whitespace-delimited words, for holding a draft to the
// voice's length rule.
func wordCount(s string) int { return len(strings.Fields(s)) }

// itoa is strconv.Itoa under a shorter name, because it appears inside string
// building where the import name is the noisiest part of the line.
func itoa(n int) string { return strconv.Itoa(n) }
