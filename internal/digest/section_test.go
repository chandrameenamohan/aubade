package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// buildOf composes a page from hand-built signals over an empty corpus. Hand
// building is how the routing rules get tested one at a time: proving that
// uncertainty beats a confident section hint needs two signals that differ in
// exactly one field, which is four lines of Go and a whole testdata directory.
func buildOf(t *testing.T, signals model.Signals, corpus *model.Corpus) *Digest {
	t.Helper()
	if corpus == nil {
		corpus = &model.Corpus{}
	}
	page, err := Build(Input{
		Corpus:  corpus,
		Signals: signals,
		Now:     mustNow(t),
		Loc:     model.Location(),
		Owner:   model.Person{Name: "Avery Chen", Email: "avery@tessera.io"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return page
}

// sectionByID finds a composed section.
func sectionByID(t *testing.T, page *Digest, id string) Section {
	t.Helper()
	for _, s := range page.Sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no section %q on the page", id)
	return Section{}
}

// unsure marks a signal uncertain.
func unsure(s model.Signal) model.Signal {
	s.Confidence = model.Unsure
	return s
}

// hinted sets a signal's section hint.
func hinted(s model.Signal, hint string) model.Signal {
	s.SectionHint = hint
	return s
}

// An uncertain signal goes under "I'm not sure" whatever its extractor hinted.
// The profile asks to be told when urgency cannot be decided and shown the
// thread; a renderer that filed the same signal under "Urgent To-Do Today"
// would be turning a caveat into an assertion.
func TestUncertaintyOutranksTheSectionHint(t *testing.T) {
	page := buildOf(t, model.Signals{
		unsure(hinted(sig("commitments:u", model.P0, nil), model.SectionUrgentToday)),
		hinted(sig("commitments:c", model.P0, nil), model.SectionUrgentToday),
	}, nil)

	notSure := sectionByID(t, page, model.SectionNotSure)
	if len(notSure.Items) != 1 || notSure.Items[0].SignalID != "commitments:u" {
		t.Fatalf(`"I'm not sure" holds %+v`, notSure.Items)
	}
	if urgent := sectionByID(t, page, model.SectionUrgentToday); len(urgent.Items) != 0 {
		// The certain one opened the page instead, as the top-ranked item.
		t.Errorf("urgent should be empty, got %+v", urgent.Items)
	}
	if page.Sections[0].Items[0].SignalID != "commitments:c" {
		t.Errorf("the certain signal should open the page, got %+v", page.Sections[0].Items)
	}
}

// An honesty-hinted signal stays in the honesty section however urgent it is: a
// caveat filed under a confident heading stops being a caveat.
func TestHonestyHintWins(t *testing.T) {
	s := hinted(sig("staleness:undated:notes/x.md", model.P0, nil,
		model.Citation{Source: model.SourceNote, Ref: "notes/x.md"}), model.SectionHonesty)
	s.Kind = model.KindStaleness
	s.Confidence = model.Unsure

	page := buildOf(t, model.Signals{s}, nil)
	honesty := sectionByID(t, page, model.SectionHonesty)
	if len(honesty.Items) != 1 {
		t.Fatalf("honesty holds %+v", honesty.Items)
	}
	if len(page.Banner) != 0 {
		t.Error("an uncertain caveat must not open the page as a banner")
	}
}

// A missing source and a stale inbox open the page. That is the honesty layer's
// only structural privilege, and it is the one the reader needs before trusting
// anything below it.
func TestStaleAndMissingSourcesOpenThePage(t *testing.T) {
	page := buildFixture(t, "stale")

	if len(page.Banner) == 0 {
		t.Fatal("a corpus with three missing sources and a four-day-old inbox has no banner")
	}
	md := page.Markdown()
	head := md[:strings.Index(md, "## ")]
	for _, want := range []string{"Heads up", "Inbox is 4 days old", "Missing source: calendar"} {
		if !strings.Contains(head, want) {
			t.Errorf("the banner does not mention %q:\n%s", want, head)
		}
	}
	if strings.Contains(md, "Sources are complete and fresh") {
		t.Error("the honesty section claims fresh sources under a staleness banner")
	}
}

// A contradiction renders both sides, each with its own citation, and resolves
// neither — "don't pick one and hide it", in the profile's own words.
func TestContradictionsRenderBothSides(t *testing.T) {
	page := buildFixture(t, "corpus")
	honesty := sectionByID(t, page, model.SectionHonesty)

	var found bool
	for _, it := range honesty.Items {
		if !strings.HasPrefix(it.SignalID, model.KindContradictions+":") {
			continue
		}
		found = true
		if len(it.Sides) < 2 {
			t.Errorf("%s renders %d side(s), want both", it.SignalID, len(it.Sides))
		}
		for _, side := range it.Sides {
			if side.Ref == "" {
				t.Errorf("%s: a side with no citation", it.SignalID)
			}
		}
	}
	if !found {
		t.Fatal("the fixture corpus plants two contradictions and the page shows none")
	}

	md := page.Markdown()
	if !strings.Contains(md, "the calendar:") || !strings.Contains(md, "the mail:") {
		t.Error("both sides should be labelled by the source that claims them")
	}
}

// The page has a bottom, and it says how far down it goes.
func TestSectionsCapAndAccountForWhatTheyDropped(t *testing.T) {
	var ss model.Signals
	for i := 0; i < capUrgent+3; i++ {
		ss = append(ss, sig("commitments:"+itoa(i), model.P2, nil,
			model.Citation{Source: model.SourceTask, Ref: "t-" + itoa(i)}))
	}
	page := buildOf(t, ss, nil)

	urgent := sectionByID(t, page, model.SectionUrgentToday)
	if len(urgent.Items) != capUrgent {
		t.Errorf("urgent rendered %d items, want the cap %d", len(urgent.Items), capUrgent)
	}
	// One of the nine opened the page, so eight reached the section and two
	// were dropped.
	if urgent.Overflow != 2 {
		t.Errorf("overflow = %d, want 2", urgent.Overflow)
	}
	if !strings.Contains(page.Markdown(), "2 more items ranked below the fold") {
		t.Error("a capped section must say how many lines it held back")
	}
}

// A dead heat for the top slot is reported rather than hidden. The page still
// opens with one item — the order has to be total — but the contest goes under
// "I'm not sure", which is the deterministic analogue of the agentic mode's
// runner disagreement.
func TestATieForTheTopSlotIsSurfaced(t *testing.T) {
	a := hinted(sig("commitments:aaa", model.P0, nil, model.Citation{Source: model.SourceTask, Ref: "t-a"}), model.SectionOneThingNow)
	b := hinted(sig("commitments:bbb", model.P0, nil, model.Citation{Source: model.SourceTask, Ref: "t-b"}), model.SectionOneThingNow)

	page := buildOf(t, model.Signals{a, b}, nil)
	if got := page.Sections[0].Items[0].SignalID; got != "commitments:aaa" {
		t.Errorf("the page opened with %s; the tie-break is the signal id", got)
	}
	notSure := sectionByID(t, page, model.SectionNotSure)
	if len(notSure.Items) != 1 {
		t.Fatalf(`a tie should add one line under "I'm not sure", got %d`, len(notSure.Items))
	}
	line := notSure.Items[0]
	if !strings.Contains(line.Lead, "tied for the top") {
		t.Errorf("the tie line does not say what it is: %q", line.Lead)
	}
	if len(line.Citations) != 2 {
		t.Errorf("the tie line should cite both candidates, got %d citations", len(line.Citations))
	}
}

// With nothing hinted at the top slot, the best certain item stands in — a
// morning has a most-important thing even when no extractor was confident
// enough to name it — and it is not also listed below.
func TestOneThingFallsBackToTheBestUrgentItem(t *testing.T) {
	page := buildOf(t, model.Signals{
		hinted(sig("commitments:low", model.P3, nil, model.Citation{Source: model.SourceTask, Ref: "t-l"}), model.SectionUrgentToday),
		hinted(sig("commitments:high", model.P0, nil, model.Citation{Source: model.SourceTask, Ref: "t-h"}), model.SectionUrgentToday),
	}, nil)

	if got := page.Sections[0].Items[0].SignalID; got != "commitments:high" {
		t.Errorf("the page opened with %s, want commitments:high", got)
	}
	urgent := sectionByID(t, page, model.SectionUrgentToday)
	if len(urgent.Items) != 1 || urgent.Items[0].SignalID != "commitments:low" {
		t.Errorf("the promoted item should not be listed twice: %+v", urgent.Items)
	}
}

// The agenda is read off the calendar, in clock order, with the shape the
// sample digest uses.
func TestAgendaRendersTodayInClockOrder(t *testing.T) {
	loc := model.Location()
	day := startOfDay(mustNow(t).In(loc))
	event := func(uid, summary string, hour, minutes int) model.CalEvent {
		start := day.Add(time.Duration(hour) * time.Hour)
		return model.CalEvent{
			UID: uid, Summary: summary, Start: start,
			End: start.Add(time.Duration(minutes) * time.Minute), Status: model.StatusConfirmed,
		}
	}
	corpus := &model.Corpus{Events: []model.CalEvent{
		event("ev-late", "Planning sync", 14, 60),
		event("ev-early", "1:1 with Jordan", 9, 30),
		{UID: "ev-off", Summary: "Cancelled thing", Start: day.Add(10 * time.Hour),
			End: day.Add(11 * time.Hour), Status: model.StatusCancelled},
	}}

	page := buildOf(t, nil, corpus)
	cal := sectionByID(t, page, model.SectionCalendar)
	if len(cal.Items) != 2 {
		t.Fatalf("agenda holds %d items, want 2 live events", len(cal.Items))
	}
	if cal.Items[0].Lead != "09:00" || cal.Items[1].Lead != "14:00" {
		t.Errorf("agenda out of clock order: %q then %q", cal.Items[0].Lead, cal.Items[1].Lead)
	}
	if !strings.Contains(page.Markdown(), "- **09:00** — 1:1 with Jordan (30m)") {
		t.Errorf("agenda line does not match the sample's shape:\n%s", page.Markdown())
	}
}

// Held-back items collapse to one line per rule. Listing thirty suppressed
// newsletters would hand the noise the profile just banned a second route onto
// the page.
func TestSuppressionsCollapseToOneLinePerRule(t *testing.T) {
	held := func(id, ref string) model.Signal {
		s := sig(id, model.P4, nil, model.Citation{Source: model.SourceEmail, Ref: ref})
		s.Kind = model.KindSuppressions
		s.SectionHint = model.SectionHonesty
		s.Detail = `labelled newsletter — "Newsletters." (profile.md:32)`
		return s
	}
	page := buildOf(t, model.Signals{
		held("suppressions:email:e-1", "e-1"),
		held("suppressions:email:e-2", "e-2"),
		held("suppressions:email:e-3", "e-3"),
	}, nil)

	honesty := sectionByID(t, page, model.SectionHonesty)
	if len(honesty.Items) != 1 {
		t.Fatalf("three suppressions under one rule rendered %d lines", len(honesty.Items))
	}
	if !strings.Contains(honesty.Items[0].Lead, "Held back 3 items") {
		t.Errorf("the line should carry the count: %q", honesty.Items[0].Lead)
	}
	if len(honesty.Items[0].Citations) > maxSuppressionCites {
		t.Errorf("a collapsed line shows at most %d receipts", maxSuppressionCites)
	}
}
