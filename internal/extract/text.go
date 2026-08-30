package extract

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text handling for the toolbox.
//
// Everything here is lexical, not statistical: fixed phrase lists, word
// boundaries, a crude stem. That is a deliberate choice, not a shortcut. The
// extractors have to be explainable line by line — a reviewer must be able to
// say "it fired because the body says 'I'll send'", and an eval trap must be
// catchable by construction. A classifier would be more forgiving and much less
// falsifiable, and evaluability is the graded axis (HLD §1).
//
// Every lexicon below is scoped to where it is read (sender fields, or a single
// sentence), never to a whole message, so a word in a signature or a quoted
// reply cannot fire a signal on its own.

var (
	wordSplitRE  = regexp.MustCompile(`[^a-z0-9]+`)
	whitespaceRE = regexp.MustCompile(`\s+`)
	subjectRE    = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|aw|sv)\s*(\[\d+\])?\s*:\s*)+`)
	sentenceRE   = regexp.MustCompile(`[.!?\n]+`)
)

// stopwords are the words too common to identify anyone or anything. The list
// is short on purpose: it exists to stop "the", "and" and friends from matching
// a company name, not to do linguistics.
var stopwords = map[string]bool{
	"about": true, "after": true, "again": true, "against": true, "already": true,
	"also": true, "and": true, "any": true, "anything": true, "are": true,
	"back": true, "because": true, "been": true, "before": true, "being": true,
	"between": true, "both": true, "but": true, "can": true, "could": true,
	"default": true, "does": true, "doing": true, "done": true, "down": true,
	"during": true, "each": true, "even": true, "ever": true, "every": true,
	"first": true, "from": true, "get": true, "give": true, "going": true,
	"have": true, "having": true, "here": true, "how": true, "into": true,
	"its": true, "just": true, "know": true, "least": true, "less": true,
	"like": true, "make": true, "many": true, "more": true, "most": true,
	"much": true, "must": true, "need": true, "next": true, "not": true,
	"now": true, "off": true, "once": true, "one": true, "only": true,
	"other": true, "our": true, "out": true, "over": true, "own": true,
	"same": true, "should": true, "since": true, "some": true, "still": true,
	"such": true, "than": true, "that": true, "the": true, "their": true,
	"them": true, "then": true, "there": true, "these": true, "they": true,
	"thing": true, "this": true, "those": true, "through": true, "too": true,
	"under": true, "until": true, "very": true, "want": true, "was": true,
	"were": true, "what": true, "when": true, "where": true, "which": true,
	"while": true, "who": true, "will": true, "with": true, "would": true,
	"you": true, "your": true,
}

// normAddr lowercases and trims an email address for comparison. Mailboxes are
// case-insensitive in practice and a missed match here is a missed signal.
func normAddr(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// domainOf returns the domain half of an address.
func domainOf(addr string) string {
	a := normAddr(addr)
	if i := strings.LastIndex(a, "@"); i >= 0 && i < len(a)-1 {
		return a[i+1:]
	}
	return ""
}

// localOf returns the mailbox half of an address.
func localOf(addr string) string {
	a := normAddr(addr)
	if i := strings.LastIndex(a, "@"); i > 0 {
		return a[:i]
	}
	return a
}

// normalizeSubject strips the reply and forward prefixes so a thread's subject
// reads the way its first message wrote it.
func normalizeSubject(s string) string {
	return strings.TrimSpace(subjectRE.ReplaceAllString(strings.TrimSpace(s), ""))
}

// collapse turns any run of whitespace into one space, so a detail line built
// from a wrapped email body reads as one sentence.
func collapse(s string) string { return strings.TrimSpace(whitespaceRE.ReplaceAllString(s, " ")) }

// stem trims a single trailing "s". It is the crudest possible stemmer and that
// is the point: it lets "newsletters" match "newsletter" and "recruiters" match
// "recruiter" without pulling in a morphology library whose behaviour nobody
// on the team could predict from the source.
func stem(w string) string {
	if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		return w[:len(w)-1]
	}
	return w
}

// words splits text into lowercased alphanumeric words.
func words(s string) []string {
	var out []string
	for _, w := range wordSplitRE.Split(strings.ToLower(s), -1) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// distinctiveTokens are the stemmed words of length ≥4 that are not stopwords —
// the ones that can plausibly identify a company, a project or a meeting.
// Order is preserved and duplicates dropped, so two callers over the same text
// always get the same slice.
func distinctiveTokens(s string) []string {
	var out []string
	for _, w := range words(s) {
		if len(w) < 4 || stopwords[w] {
			continue
		}
		st := stem(w)
		if !slices.Contains(out, st) {
			out = append(out, st)
		}
	}
	return out
}

// searchTokens are the query terms for `tool search`: everything of length ≥2,
// lowercased. Search is a user-directed tool, so it keeps short words the
// extractors drop — someone searching "Q1" means Q1.
func searchTokens(q string) []string {
	var out []string
	for _, w := range words(q) {
		if len(w) >= 2 && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// overlapCount counts how many of want's tokens appear in have.
func overlapCount(have, want []string) int {
	n := 0
	for _, w := range want {
		if slices.Contains(have, w) {
			n++
		}
	}
	return n
}

// containsWord reports whether haystack contains needle as a whole word or
// phrase, with boundaries on both ends. Substring matching would let "sent"
// fire on "absent"; that is exactly the class of false positive an honesty
// layer cannot afford.
func containsWord(haystack, needle string) bool {
	h, n := normQuotes(strings.ToLower(haystack)), normQuotes(strings.ToLower(needle))
	if n == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(h[i:], n)
		if j < 0 {
			return false
		}
		j += i
		if boundaryBefore(h, j) && boundaryAfter(h, j+len(n)) {
			return true
		}
		i = j + 1
		if i >= len(h) {
			return false
		}
	}
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

// normQuotes folds the curly apostrophe onto the straight one. Mail clients
// emit both, often in the same thread, and "I’ll send it" has to match the same
// promise lexicon as "I'll send it".
func normQuotes(s string) string {
	if !strings.ContainsAny(s, "‘’“”") {
		return s
	}
	return strings.NewReplacer("‘", "'", "’", "'", "“", `"`, "”", `"`).Replace(s)
}

// containsAny reports whether any phrase appears in s as a whole word.
func containsAny(s string, phrases []string) bool {
	_, ok := firstMatch(s, phrases)
	return ok
}

// firstMatch returns the first phrase from the list that appears in s. The list
// order is the priority order, so callers can put the most specific phrase
// first and get it back for the detail line.
func firstMatch(s string, phrases []string) (string, bool) {
	for _, p := range phrases {
		if containsWord(s, p) {
			return p, true
		}
	}
	return "", false
}

// sentences splits a body into trimmed sentences. Extractor lexicons are
// matched per sentence: "I'll send the deck. Separately, the board meeting is
// Thursday" is a promise with no deadline and a date with no promise, and
// matching over the whole body would happily fuse them into a commitment that
// nobody made.
func sentences(body string) []string {
	var out []string
	for _, s := range sentenceRE.Split(body, -1) {
		if t := collapse(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// askPhrases mark a message that leaves something open. The bare "?" is handled
// separately because it is punctuation, not a word.
var askPhrases = []string{
	"can you", "could you", "would you", "will you", "are you able",
	"please send", "please confirm", "please review", "please approve",
	"let me know", "let us know", "need a", "need the", "need your",
	"waiting on", "waiting for", "any update", "any word", "status on",
	"thoughts", "what do you think", "yes or no", "confirm", "approve",
	"sign off", "sign-off", "can we", "does that work", "are we", "is the",
	"when can", "what time", "who is", "how many", "which option",
}

// asksSomething reports whether text leaves a ball in someone's court.
func asksSomething(text string) bool {
	if strings.Contains(text, "?") {
		return true
	}
	return containsAny(text, askPhrases)
}

// decisionPhrases mark an ask that belongs under "decisions & approvals" rather
// than plain urgency.
var decisionPhrases = []string{
	"approve", "approval", "sign off", "sign-off", "signature", "countersign",
	"ok to", "okay to", "go ahead", "green light", "your call", "decide",
	"decision", "which option", "option a", "option b", "pick one",
	"authorize", "budget approval",
}

// firstQuestion returns the first sentence that asks something — the line a
// dispatchable reply would be answering.
func firstQuestion(body string) string {
	for _, s := range sentences(body) {
		if asksSomething(s) {
			return s
		}
	}
	return ""
}

// snippet returns a short window of text around the first matching token, so a
// search hit shows why it matched.
func snippet(body string, tokens []string) string {
	b := collapse(body)
	if b == "" {
		return ""
	}
	lower := strings.ToLower(b)
	idx := -1
	for _, tok := range tokens {
		if i := strings.Index(lower, tok); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx < 0 {
		idx = 0
	}
	const half = 70
	start := max(idx-half, 0)
	end := min(idx+half, len(b))
	out := b[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(b) {
		out += "…"
	}
	return out
}

// truncate shortens s for a signal detail, cutting on a word boundary.
func truncate(s string, n int) string {
	s = collapse(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// quote renders a fragment of someone's own words for a detail line.
func quote(s string) string { return `"` + truncate(s, 120) + `"` }

// capitalize upper-cases the first rune, for a phrase reused mid-sentence and
// at the start of one.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
