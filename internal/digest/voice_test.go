package digest

import (
	"slices"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/styles"
)

// averyTone is the tone section of the fixture profile, with the line numbers
// it actually occupies in testdata/corpus/profile.md.
func averyTone() *model.Profile {
	return &model.Profile{
		Path: "profile.md",
		ToneRules: []model.Rule{
			{Text: `Short. Lowercase greetings or none at all. Sign off with "Avery" or nothing.`, Line: 46},
			{Text: `No "I hope this email finds you well." No "circling back."`, Line: 47},
			{Text: "For investors during the raise, slightly more polished. Still short.", Line: 48},
			{Text: "For Sam: don't draft. Surface and let me write.", Line: 49},
			{Text: "Warmth comes from specificity, not adjectives.", Line: 50},
		},
	}
}

// The base file is a product asset, not documentation: it has to be readable
// and it has to contain the principles the drafter claims to apply.
func TestBaseVoiceIsEmbeddedAndParsed(t *testing.T) {
	if strings.TrimSpace(styles.DefaultVoice) == "" {
		t.Fatal("styles/default-voice.md did not embed")
	}
	if n := len(bulletsOf(styles.DefaultVoice)); n < 20 {
		t.Errorf("the base voice parsed to %d bullets, which is not the file", n)
	}

	v := LoadVoice(nil)
	if !v.CutAdverbs {
		t.Errorf(`"Cut adverbs" did not reach the drafter: %+v`, v)
	}
	if v.MaxWords != shortMaxWords {
		t.Errorf("the base file's length rule did not apply: MaxWords = %d", v.MaxWords)
	}
	if v.Greeting != GreetingSentence {
		t.Errorf("with no profile the greeting stays the neutral default, got %q", v.Greeting)
	}
}

// The guard, which is the reason CutAdverbs and the banned list are fields
// rather than commentary: a sentence that breaks the user's own rules does not
// get shown as if it were their voice.
func TestVoiceRefusesItsOwnViolations(t *testing.T) {
	v := LoadVoice(averyTone())

	if broken := v.Check("hi renee,\n\ncircling back on this.\n\nAvery"); len(broken) == 0 {
		t.Error("a banned phrase passed the check")
	}
	if broken := v.Check("quickly confirming."); len(broken) == 0 {
		t.Error("an adverb passed the check under a cut-adverbs rule")
	}
	if broken := v.Check(strings.Repeat("word ", v.MaxWords+5)); len(broken) == 0 {
		t.Error("an over-long draft passed the check")
	}
	if broken := v.Check("hi renee,\n\non — answering here.\n\n[your answer]\n\nAvery"); len(broken) != 0 {
		t.Errorf("the drafter's own template does not satisfy the voice it claims: %v", broken)
	}
}

// The layering, which is the product decision: the profile wins wherever it
// speaks, and only there.
func TestProfileTonRulesOverrideTheBase(t *testing.T) {
	base := LoadVoice(nil)
	v := LoadVoice(averyTone())

	if v.Greeting != GreetingLowercase {
		t.Errorf("greeting = %q, want lowercase (profile.md:46)", v.Greeting)
	}
	if v.Signoff != "Avery" {
		t.Errorf("signoff = %q, want Avery (profile.md:46)", v.Signoff)
	}
	if !slices.Contains(v.Banned, "I hope this email finds you well") || !slices.Contains(v.Banned, "circling back") {
		t.Errorf("banned phrases = %v, want both quoted phrases from profile.md:47", v.Banned)
	}
	if len(v.NoDraft) != 1 || v.NoDraft[0] != "Sam" {
		t.Errorf("no-draft list = %v, want [Sam] (profile.md:49)", v.NoDraft)
	}
	if v.MaxWords != shortMaxWords || base.MaxWords != shortMaxWords {
		t.Errorf(`both documents ask for short; MaxWords = %d (profile) / %d (base), want %d`,
			v.MaxWords, base.MaxWords, shortMaxWords)
	}
	// The base principles the profile said nothing about are still in force.
	if !v.CutAdverbs {
		t.Errorf("the profile silently dropped a base principle: %+v", v)
	}
}

// A tone rule this parser cannot act on is recorded, not guessed at — and it
// reaches the page, because a rule we silently ignored is a promise we silently
// broke.
func TestUnparsedToneRulesAreSurfaced(t *testing.T) {
	v := LoadVoice(averyTone())
	if len(v.Unhandled) != 1 || !strings.HasPrefix(v.Unhandled[0].Text, "Warmth comes") {
		t.Fatalf("unhandled = %+v, want the one bullet with no mechanical effect", v.Unhandled)
	}

	page := buildOf(t, nil, &model.Corpus{Profile: averyTone()})
	md := page.Markdown()
	if !strings.Contains(md, "Warmth comes from specificity") || !strings.Contains(md, "profile.md:50") {
		t.Errorf("an unapplied tone rule should be stated in the honesty section:\n%s", md)
	}
}

// "For Sam: don't draft. Surface and let me write." — matched on the person,
// not on a substring, so Samantha is not Sam.
func TestNeverDraftForTheProtectedPerson(t *testing.T) {
	v := LoadVoice(averyTone())

	if ok, rule := v.DraftsFor(model.Person{Name: "Sam Park", Email: "sam@parkhouse.example"}); ok {
		t.Error("drafted for Sam")
	} else if rule.Ref() != "profile.md:49" {
		t.Errorf("the refusal cites %q, want profile.md:49", rule.Ref())
	}
	if ok, _ := v.DraftsFor(model.Person{Name: "Samantha Okoye", Email: "samantha@elsewhere.example"}); !ok {
		t.Error("Samantha is not Sam; the no-draft rule matched a substring")
	}
}

// "For investors during the raise, slightly more polished" resolves through the
// contact list's own words, so the user never has to restate who their
// investors are inside a tone bullet.
func TestPolishedRegisterResolvesThroughTheProfileEntry(t *testing.T) {
	v := LoadVoice(averyTone())
	marcus := model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"}
	entry := &model.ProfilePerson{
		Name: "Marcus Webb", Priority: model.P0, Line: 19,
		Note: "Series A lead at Inflection Point Ventures — P0 during the raise.",
	}

	if ok, _ := v.IsPolished(marcus, entry); !ok {
		t.Error("Marcus's own profile line names a venture fund and did not match the investor rule")
	}
	renee := model.Person{Name: "Renee Tan", Email: "renee@halberd.example"}
	if ok, _ := v.IsPolished(renee, &model.ProfilePerson{Name: "Renee Tan", Role: "procurement lead"}); ok {
		t.Error("a customer should not get the investor register")
	}
}

// The drafts on the real page: the register switches per correspondent, the
// protected person is surfaced without one, and nothing invents an answer.
func TestDraftsFollowTheVoiceAndInventNothing(t *testing.T) {
	md := buildFixture(t, "corpus").Markdown()

	for _, want := range []string{
		"hi renee,",   // lowercase greeting, profile.md:46
		"Hi Marcus,",  // investor register keeps the capital
		"\n    Avery", // sign-off, profile.md:46
		"Not drafted — \"For Sam: don't draft. Surface and let me write.\" (profile.md:49)",
		AnswerPlaceholder,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the page never renders %q", want)
		}
	}

	// Nothing the profile bans may appear in a draft, and no draft may claim to
	// know the answer.
	v := LoadVoice(averyTone())
	for _, banned := range v.Banned {
		if strings.Contains(strings.ToLower(md), strings.ToLower(banned)) {
			t.Errorf("a banned phrase reached the page: %q", banned)
		}
	}
}

// Every draft the real page emits stays inside the voice's length rule and
// leaves the answer blank.
func TestDraftsStayShortAndLeaveTheAnswerBlank(t *testing.T) {
	page := buildFixture(t, "corpus")

	var checked int
	for _, s := range page.Sections {
		for _, it := range s.Items {
			if it.Draft == nil || it.Draft.Skipped {
				continue
			}
			checked++
			if n := wordCount(it.Draft.Body); n > page.Voice.MaxWords {
				t.Errorf("draft to %s is %d words, over the %d this voice allows",
					it.Draft.To.Name, n, page.Voice.MaxWords)
			}
			if !strings.Contains(it.Draft.Body, AnswerPlaceholder) {
				t.Errorf("draft to %s answers the question itself", it.Draft.To.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the fixture corpus produced no drafts to check")
	}
}
