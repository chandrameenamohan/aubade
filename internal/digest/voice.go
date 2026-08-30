package digest

import (
	"regexp"
	"slices"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/styles"
)

// The drafting voice is two layers, and the layering is the product decision
// (SPEC §5): aubade's built-in voice — styles/default-voice.md, distilled Shaan
// Puri drafting principles — is the base, and the user's own profile.md tone
// rules override it wherever they speak. The default file says so itself:
// "Precedence: Rules in the user's profile.md ALWAYS override this file."
//
// The implementation is deliberately one classifier over two documents rather
// than two parsers. Both files are bullet lists written by a person; the same
// function reads a bullet and decides what it changes; the profile is applied
// second, so the last writer wins. That makes precedence a property of the call
// order — one line, visible — instead of a table nobody maintains.
//
// A bullet the classifier does not understand is recorded, not guessed at.
// Unhandled bullets *from the user's profile* are surfaced in the digest's
// honesty section, exactly as an unparsed suppression rule is: a tone rule we
// silently ignored is a promise we silently broke. Unhandled bullets from our
// own base file are not surfaced — most of them are guidance for a model
// composing prose, and the template drafter has no prose to compose.
//
// One thing this file will not do is fill in the answer. A dispatchable is a
// question somebody asked; the corpus contains the question, not the reply.
// The draft therefore carries the ask, the voice, and a marked blank — never a
// guess dressed as the user's own words.

// GreetingStyle is how a draft opens.
type GreetingStyle string

// The three greeting styles. Sentence is the base voice's default; the profile
// can lower it or remove it.
const (
	GreetingSentence  GreetingStyle = "sentence"  // "Hi Renee,"
	GreetingLowercase GreetingStyle = "lowercase" // "hi renee,"
	GreetingNone      GreetingStyle = "none"
)

// AnswerPlaceholder is the blank a drafted reply leaves for the one thing
// aubade cannot know. It is a fixed string so the eval, the tests and the user
// all recognise the same marker.
const AnswerPlaceholder = "[your answer]"

// VoiceRule is one bullet that changed the voice, kept with where it came from
// so a draft can cite the line that shaped it.
type VoiceRule struct {
	Text   string `json:"text"`
	Path   string `json:"path"`   // "styles/default-voice.md" or "profile.md"
	Line   int    `json:"line"`   // 1-based line in that file
	Effect string `json:"effect"` // what it changed, in a word or two
}

// Ref renders the rule's provenance: "profile.md:47".
func (r VoiceRule) Ref() string { return ruleRef(r.Path, r.Line) }

// Voice is the resolved drafting voice: the base file's principles with the
// user's tone rules applied over the top.
//
// Every field on it changes an actual draft. A principle that cannot be applied
// mechanically — "warmth comes from specificity, not adjectives" — is not
// modelled as a field that nothing reads; it stays an unhandled rule, and if it
// is the user's own it is stated on the page.
type Voice struct {
	Greeting    GreetingStyle
	Signoff     string
	Banned      []string
	Polished    []string
	NoDraft     []string
	MaxWords    int
	CutAdverbs  bool
	Applied     []VoiceRule
	Unhandled   []model.Rule
	ProfilePath string
}

// baseDefaults are the voice before either document is read. They are the
// neutral business-email defaults the base file's principles then sharpen; they
// exist so a corpus with no profile and an unreadable base file still drafts
// something a person could send.
const (
	baseSignoff  = ""
	baseMaxWords = 90
)

// LoadVoice resolves the voice for one corpus: the embedded base file first,
// then the profile's tone rules over the top.
func LoadVoice(profile *model.Profile) *Voice {
	v := &Voice{
		Greeting:    GreetingSentence,
		Signoff:     baseSignoff,
		MaxWords:    baseMaxWords,
		ProfilePath: "profile.md",
	}

	for _, r := range bulletsOf(styles.DefaultVoice) {
		v.apply(r, styles.DefaultVoicePath, false)
	}
	if profile == nil {
		return v
	}
	if p := strings.TrimSpace(profile.Path); p != "" {
		v.ProfilePath = p
	}
	for _, r := range profile.ToneRules {
		v.apply(r, v.ProfilePath, true)
	}
	return v
}

// bulletRE matches a markdown list bullet.
var bulletRE = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)

// quotedRE pulls the phrases a rule bans out of the rule's own words, straight
// and curly quotes alike: No "I hope this email finds you well."
var quotedRE = regexp.MustCompile(`["“]([^"”]{2,80})["”]`)

// audienceRE matches an audience-scoped rule: "For Sam: don't draft", "For
// investors during the raise, slightly more polished".
var audienceRE = regexp.MustCompile(`(?i)^for\s+([^:,.]+)[:,]`)

// bulletsOf splits a markdown document into its list bullets, keeping the line
// each came from so a rule can cite itself.
func bulletsOf(doc string) []model.Rule {
	var out []model.Rule
	for i, line := range strings.Split(doc, "\n") {
		m := bulletRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if text := strings.TrimSpace(m[1]); text != "" {
			out = append(out, model.Rule{Text: text, Line: i + 1})
		}
	}
	return out
}

// apply folds one bullet into the voice. record says whether an unhandled
// bullet is worth surfacing — true for the user's own rules, false for the base
// file, most of whose guidance is for a model writing prose rather than for a
// template filling a blank.
func (v *Voice) apply(r model.Rule, path string, record bool) {
	effects := v.classify(r)
	if len(effects) == 0 {
		if record {
			v.Unhandled = append(v.Unhandled, r)
		}
		return
	}
	for _, e := range effects {
		v.Applied = append(v.Applied, VoiceRule{Text: r.Text, Path: path, Line: r.Line, Effect: e})
	}
}

// classify reads one bullet and mutates the voice, returning a short name for
// each thing it changed. A bullet may legitimately say two things — "Short.
// Lowercase greetings or none at all. Sign off with 'Avery' or nothing." says
// three — so the return is a list.
func (v *Voice) classify(r model.Rule) []string {
	text := strings.ToLower(r.Text)
	var effects []string

	if audience, ok := audienceOf(r.Text); ok {
		switch {
		case containsAny(text, "don't draft", "do not draft", "dont draft"):
			v.NoDraft = appendUnique(v.NoDraft, audience)
			return []string{"never draft for " + audience}
		case strings.Contains(text, "polished"):
			v.Polished = appendUnique(v.Polished, distinctiveWords(audience)...)
			return []string{"polished for " + audience}
		}
	}

	if strings.Contains(text, "greeting") {
		switch {
		case strings.Contains(text, "lowercase"):
			v.Greeting = GreetingLowercase
			effects = append(effects, "lowercase greetings")
		case containsAny(text, "no greeting", "none at all", "skip the greeting"):
			v.Greeting = GreetingNone
			effects = append(effects, "no greeting")
		}
	}

	if containsAny(text, "sign off", "sign-off", "signoff") {
		if names := quotedRE.FindAllStringSubmatch(r.Text, -1); len(names) > 0 {
			v.Signoff = strings.TrimSpace(names[0][1])
			effects = append(effects, "sign off "+v.Signoff)
		} else if containsAny(text, "no signature", "nothing") {
			v.Signoff = ""
			effects = append(effects, "no sign-off")
		}
	}

	// A rule that bans a phrase quotes the phrase. Reading the ban out of the
	// user's own quotation marks is what lets "No 'circling back.'" work
	// without a lexicon of business clichés maintained by us.
	if strings.HasPrefix(text, "no ") || strings.Contains(text, " no \"") || strings.Contains(text, " no “") {
		for _, m := range quotedRE.FindAllStringSubmatch(r.Text, -1) {
			phrase := strings.Trim(strings.TrimSpace(m[1]), ".!?")
			if phrase == "" {
				continue
			}
			v.Banned = appendUnique(v.Banned, phrase)
			effects = append(effects, "ban "+phrase)
		}
	}

	if containsAny(text, "short.", "keep it short", "as short as") {
		v.MaxWords = shortMaxWords
		effects = append(effects, "short")
	}
	if strings.Contains(text, "adverb") {
		v.CutAdverbs = true
		effects = append(effects, "cut adverbs")
	}
	return effects
}

// Check reports the voice rules a sentence aubade wrote would break.
//
// It is a guard rather than a lint: a draft goes out under the user's name, so
// a template that broke the user's own tone rules must not be shown as if it
// were in their voice. The composer would rather surface the item undrafted
// than hand over a sentence they said not to write.
//
// Only aubade's own words are checked. A quotation of what the counterparty
// asked is theirs — a recruiter who opens with "circling back" has not made the
// reply break the rule against writing it.
func (v *Voice) Check(text string) []string {
	var out []string
	lower := strings.ToLower(text)
	for _, phrase := range v.Banned {
		if containsWholeWord(lower, phrase) {
			out = append(out, "banned phrase "+quoted(phrase))
		}
	}
	if v.CutAdverbs {
		for _, w := range strings.Fields(lower) {
			word := strings.Trim(w, `.,;:!?"'()`)
			if len(word) > 4 && strings.HasSuffix(word, "ly") {
				out = append(out, "adverb "+quoted(word))
			}
		}
	}
	if n := wordCount(text); v.MaxWords > 0 && n > v.MaxWords {
		out = append(out, itoa(n)+" words, over the "+itoa(v.MaxWords)+" this voice allows")
	}
	return out
}

// shortMaxWords is what "short" means in words. It is a number rather than a
// feeling so a test can hold a draft to it.
const shortMaxWords = 45

// audienceOf pulls the audience out of an audience-scoped rule.
func audienceOf(text string) (string, bool) {
	m := audienceRE.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", false
	}
	audience := strings.TrimSpace(m[1])
	if audience == "" {
		return "", false
	}
	return audience, true
}

// DraftsFor reports whether this voice will draft a reply to the given person,
// and the rule that says otherwise when it will not.
func (v *Voice) DraftsFor(p model.Person) (bool, VoiceRule) {
	haystack := strings.ToLower(p.Name + " " + p.Email)
	for _, blocked := range v.NoDraft {
		for _, token := range distinctiveWords(blocked) {
			if !containsWholeWord(haystack, token) {
				continue
			}
			return false, v.ruleFor("never draft for " + blocked)
		}
	}
	return true, VoiceRule{}
}

// IsPolished reports whether this correspondent gets the more polished
// register, and the rule that decided it. The match is on the profile's own
// words about them — "Series A lead at Inflection Point Ventures" answers to
// the "investors" rule — so the user never has to restate their contact list
// inside a tone bullet.
func (v *Voice) IsPolished(p model.Person, entry *model.ProfilePerson) (bool, VoiceRule) {
	if len(v.Polished) == 0 {
		return false, VoiceRule{}
	}
	haystack := strings.ToLower(p.Name + " " + p.Email)
	if entry != nil {
		haystack += " " + strings.ToLower(entry.Name+" "+entry.Role+" "+entry.Note)
	}
	for _, want := range v.Polished {
		for _, token := range polishedSynonyms(want) {
			if containsWholeWord(haystack, token) {
				return true, v.ruleFor("polished")
			}
		}
	}
	return false, VoiceRule{}
}

// polishedSynonyms expands an audience word into the words a profile actually
// uses for that audience. "investors" is how a tone rule names them; "Series A
// lead at Inflection Point Ventures" is how the contact list does.
func polishedSynonyms(word string) []string {
	base := []string{word, strings.TrimSuffix(word, "s")}
	if strings.HasPrefix(word, "invest") {
		base = append(base, "investor", "vc", "ventures", "venture", "capital", "seed", "board")
	}
	return base
}

// ruleFor finds the applied rule whose effect starts with the given prefix, so
// a draft can cite the line that shaped it.
func (v *Voice) ruleFor(prefix string) VoiceRule {
	for _, r := range v.Applied {
		if strings.HasPrefix(r.Effect, prefix) {
			return r
		}
	}
	return VoiceRule{}
}

// Overrides are the applied rules that came from the user's profile rather than
// from the base file — the layer that wins, one entry per source line, in
// document order. A draft cites them so the reader can see which of their own
// sentences shaped it.
func (v *Voice) Overrides() []VoiceRule {
	var (
		out  []VoiceRule
		seen []string
	)
	for _, r := range v.Applied {
		if r.Path != v.ProfilePath || slices.Contains(seen, r.Ref()) {
			continue
		}
		seen = append(seen, r.Ref())
		out = append(out, r)
	}
	return out
}

// ProfileRules counts the tone rules that came from the user rather than from
// the base file — the number the footer reports.
func (v *Voice) ProfileRules() int { return len(v.Overrides()) }

// ruleRef renders "profile.md:47".
func ruleRef(path string, line int) string {
	if line <= 0 {
		return path
	}
	return path + ":" + itoa(line)
}
