package datagen

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The pinned anchor: the seed and date the regression suite generates with
// (SPEC "End-to-end verification scenario"). It is a Sunday, which is on
// purpose — every helper that has to reason about business days or weekdays is
// exercised at its least convenient anchor by default.
const (
	pinnedSeed = 42
	pinnedDay  = "2026-08-30"
)

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", day, model.Location())
	if err != nil {
		t.Fatalf("parse %q: %v", day, err)
	}
	return d
}

func mustBuild(t *testing.T) *Plan {
	t.Helper()
	plan, err := Build(Config{Seed: pinnedSeed, Today: mustDay(t, pinnedDay)})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

// Same seed, same day, byte-identical output — the property the golden digest
// and the whole regression suite rest on. Asserted on the marshalled plan
// rather than on the struct, because bytes are what the writer will emit and
// what a diff will show.
func TestBuildIsDeterministic(t *testing.T) {
	first, second := mustBuild(t), mustBuild(t)

	a, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("two builds with the same seed and day produced different plans")
	}
}

// The seed moves detail; it must never move the exam. A trap that only appears
// for some seeds is a flaky test wearing a dataset's clothes, and no golden
// digest could survive it.
func TestSeedChangesDetailNotTheAnswerKey(t *testing.T) {
	day := mustDay(t, pinnedDay)
	base, err := Build(Config{Seed: pinnedSeed, Today: day})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	other, err := Build(Config{Seed: pinnedSeed + 1, Today: day})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !reflect.DeepEqual(base.Traps, other.Traps) {
		t.Error("the answer key changed with the seed; traps must be scripted, not sampled")
	}
	if len(base.Emails) != len(other.Emails) {
		t.Fatalf("seed changed the number of emails: %d vs %d", len(base.Emails), len(other.Emails))
	}
	differs := false
	for i := range base.Emails {
		if !base.Emails[i].TS.Equal(other.Emails[i].TS) || base.Emails[i].Body != other.Emails[i].Body {
			differs = true
			break
		}
	}
	if !differs {
		t.Error("the seed changed nothing at all; it is not wired to the detail it is supposed to drive")
	}
}

// Every extractor must have at least one task behind it. An extractor with no
// task is one that can break without the scorecard noticing, which is the
// failure mode the sabotage pass exists to catch and this test exists to
// prevent.
func TestCatalogCoversEveryExtractor(t *testing.T) {
	got := mustBuild(t).Traps.SignalKinds()
	if !slices.Equal(got, model.KnownKinds) {
		t.Errorf("expect.signal_kind coverage = %v, want every extractor %v", got, model.KnownKinds)
	}
}

// Every published trap category must be planted by something. A category
// constant nobody uses is a category the answer key claims to test and does
// not.
func TestCatalogCoversEveryCategory(t *testing.T) {
	plan := mustBuild(t)
	for _, category := range KnownCategories {
		if !slices.ContainsFunc(plan.Traps, func(tr Trap) bool { return tr.Kind == category }) {
			t.Errorf("no trap plants category %q", category)
		}
	}
}

// SPEC §1: at least twelve positive traps spanning every extractor category,
// and at least four negatives.
func TestCatalogSize(t *testing.T) {
	plan := mustBuild(t)
	if n := len(plan.Traps.Positive()); n < 12 {
		t.Errorf("positive traps = %d, want at least 12", n)
	}
	if n := len(plan.Traps.Negative()); n < 4 {
		t.Errorf("negative traps = %d, want at least 4", n)
	}
	seen := map[string]bool{}
	for _, tr := range plan.Traps {
		if seen[tr.ID] {
			t.Errorf("duplicate trap id %q", tr.ID)
		}
		seen[tr.ID] = true
	}
}

// Every planted_ref must resolve to something the same scenario actually wrote.
// Build enforces it; this asserts it independently, and asserts that Resolve
// says no when it should — a resolver that returns true for everything would
// make the Build check vacuous.
func TestPlantedRefsResolve(t *testing.T) {
	plan := mustBuild(t)
	for _, tr := range plan.Traps {
		for _, ref := range tr.PlantedRefs {
			if !plan.Resolve(ref) {
				t.Errorf("trap %s: planted_ref %s:%s resolves to nothing", tr.ID, ref.Source, ref.Ref)
			}
		}
	}

	bogus := []model.Citation{
		{Source: model.SourceEmail, Ref: "m-does-not-exist"},
		{Source: model.SourceCalendar, Ref: "ev-does-not-exist"},
		{Source: model.SourceNote, Ref: "notes/does-not-exist.md"},
		{Source: model.SourceTask, Ref: "t-does-not-exist"},
		{Source: model.Source("mailbox"), Ref: "m-capt-01"},
	}
	for _, ref := range bogus {
		if plan.Resolve(ref) {
			t.Errorf("Resolve(%s:%s) = true, want false", ref.Source, ref.Ref)
		}
	}
}

// Every keyword a trap will be graded on has to be quotable from that trap's
// own cited evidence. Otherwise the digest can only pass by saying something
// the corpus never said — a task that cannot be solved from the data is a
// broken task, not a hard one (EVAL-PRINCIPLES #7).
func TestTrapKeywordsArePlanted(t *testing.T) {
	plan := mustBuild(t)
	for _, tr := range plan.Traps {
		var evidence strings.Builder
		for _, ref := range tr.PlantedRefs {
			evidence.WriteString(plan.text(ref))
			evidence.WriteString("\n")
		}
		haystack := strings.ToLower(evidence.String())
		for _, kw := range tr.Expect.Keywords {
			if !strings.Contains(haystack, strings.ToLower(kw)) {
				t.Errorf("trap %s: keyword %q appears in none of its planted artifacts", tr.ID, kw)
			}
		}
	}
}

// text renders everything a digest could legitimately quote from one cited
// artifact. Addresses are included because "Brightmoor" is as much a fact about
// ines@brightmoor.example as it is about the signature under it.
func (p *Plan) text(c model.Citation) string {
	var b strings.Builder
	writePerson := func(who model.Person) {
		b.WriteString(who.Name + " " + who.Email + " ")
	}
	switch c.Source {
	case model.SourceEmail:
		for _, e := range p.Emails {
			if e.ID != c.Ref {
				continue
			}
			b.WriteString(e.Subject + " " + e.Body + " ")
			writePerson(e.From)
			for _, who := range slices.Concat(e.To, e.CC) {
				writePerson(who)
			}
		}
	case model.SourceCalendar:
		for _, e := range p.Events {
			if e.UID != c.Ref {
				continue
			}
			b.WriteString(e.Summary + " " + e.Description + " " + e.Location + " ")
			writePerson(e.Organizer)
			for _, a := range e.Attendees {
				writePerson(a.Person)
			}
		}
	case model.SourceNote:
		for _, n := range p.Notes {
			if n.Path == c.Ref {
				b.WriteString(n.Path + " " + n.Title + " " + n.Body + " ")
			}
		}
	case model.SourceTask:
		for _, task := range p.Tasks {
			if task.ID == c.Ref {
				b.WriteString(task.ID + " " + task.Title + " ")
			}
		}
	}
	return b.String()
}

// The corpus has to be a corpus: thirty days of history, not a handful of
// fixtures around today. Cadence and quiet-thread traps are only detectable
// against a distribution.
func TestCorpusSpansThirtyDays(t *testing.T) {
	plan := mustBuild(t)
	if len(plan.Emails) == 0 {
		t.Fatal("no emails planted")
	}
	oldest := plan.Emails[0].TS
	if age := plan.Today.Sub(oldest); age < 25*24*time.Hour {
		t.Errorf("oldest planted email is %v old, want the corpus to reach back toward %d days", age, CorpusDays)
	}
	if newest := plan.Emails[len(plan.Emails)-1].TS; newest.After(plan.Today.AddDate(0, 0, 1)) {
		t.Errorf("newest planted email %s is in the future relative to today %s", newest, plan.Today)
	}
}

// Emails are handed on in timeline order. Threads have to interleave: a thread
// that went quiet is only visible as quiet against everything that happened
// while it was silent.
func TestEmailsAreInTimelineOrder(t *testing.T) {
	plan := mustBuild(t)
	for i := 1; i < len(plan.Emails); i++ {
		prev, cur := plan.Emails[i-1], plan.Emails[i]
		if cur.TS.Before(prev.TS) {
			t.Fatalf("emails out of order at %d: %s before %s", i, cur.ID, prev.ID)
		}
		if cur.TS.Equal(prev.TS) && cur.ID < prev.ID {
			t.Fatalf("tied timestamps not broken by id at %d: %s before %s", i, cur.ID, prev.ID)
		}
	}

	threads := map[string]bool{}
	interleaved := false
	var last string
	for _, e := range plan.Emails {
		if last != "" && e.ThreadID != last && threads[e.ThreadID] {
			interleaved = true
			break
		}
		threads[e.ThreadID] = true
		last = e.ThreadID
	}
	if !interleaved {
		t.Error("no thread is interrupted by another; the scenarios are not woven into one timeline")
	}
}

// A trap timed in raw days sits on a different side of the three-business-day
// rule depending on which weekday the anchor lands on. These are the two
// anchors that would disagree.
func TestQuietTrapsHoldForAnyAnchor(t *testing.T) {
	for _, day := range []string{"2026-08-30", "2026-09-03", "2026-09-05"} {
		t.Run(day, func(t *testing.T) {
			plan, err := Build(Config{Seed: pinnedSeed, Today: mustDay(t, day)})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			over := plan.emailByID(t, "m-aperture-04")
			under := plan.emailByID(t, "m-brightmoor-02")
			if n := businessDaysSince(over.TS, plan.Today); n <= 3 {
				t.Errorf("quiet-investor trap is %d business days old, want more than 3", n)
			}
			if n := businessDaysSince(under.TS, plan.Today); n >= 3 {
				t.Errorf("below-threshold trap is %d business days old, want fewer than 3", n)
			}
		})
	}
}

func (p *Plan) emailByID(t *testing.T, id string) model.Email {
	t.Helper()
	for _, e := range p.Emails {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no email %q in the plan", id)
	return model.Email{}
}

// businessDaysSince counts Mon-Fri days after ts up to and including today. It
// is written independently of Script.BusinessDaysAgo on purpose: a test that
// reuses the implementation it is checking proves only that the code is
// consistent with itself.
func businessDaysSince(ts, today time.Time) int {
	day := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, ts.Location())
	n := 0
	for day.Before(today) {
		day = day.AddDate(0, 0, 1)
		switch day.Weekday() {
		case time.Saturday, time.Sunday:
		default:
			n++
		}
	}
	return n
}

// The deep-work rule is written in weekdays, so the violation has to land on a
// Tuesday or a Thursday whatever the anchor is, inside the 9-11 block, and on a
// day the block itself exists.
func TestDeepWorkViolationLandsInTheBlock(t *testing.T) {
	for _, day := range []string{"2026-08-30", "2026-09-01", "2026-09-03"} {
		t.Run(day, func(t *testing.T) {
			plan, err := Build(Config{Seed: pinnedSeed, Today: mustDay(t, day)})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			trap, ok := plan.Traps.ByID("conflict-deep-work-block")
			if !ok {
				t.Fatal("no deep-work trap in the catalog")
			}

			var block, meeting model.CalEvent
			for _, ref := range trap.PlantedRefs {
				for _, ev := range plan.Events {
					if ev.UID != ref.Ref {
						continue
					}
					if strings.HasPrefix(ev.UID, "ev-deep-work-") {
						block = ev
					} else {
						meeting = ev
					}
				}
			}
			if block.UID == "" || meeting.UID == "" {
				t.Fatalf("trap cites %d events; want one block and one meeting", len(trap.PlantedRefs))
			}
			switch meeting.Start.Weekday() {
			case time.Tuesday, time.Thursday:
			default:
				t.Errorf("violating meeting is on a %s, not a deep-work day", meeting.Start.Weekday())
			}
			if !meeting.Start.Before(block.End) || !block.Start.Before(meeting.End) {
				t.Errorf("meeting %s..%s does not overlap the block %s..%s",
					meeting.Start, meeting.End, block.Start, block.End)
			}
			if !meeting.Created.After(plan.Today.AddDate(0, 0, -2)) {
				t.Errorf("violating meeting was created %s; it is only news if it was added recently", meeting.Created)
			}
		})
	}
}

// A zero Today means the system date, normalized to midnight in the corpus
// zone. Anything else would give the same seed a different corpus depending on
// what time of day the generator ran.
func TestZeroTodayNormalizesToMidnight(t *testing.T) {
	plan, err := Build(Config{Seed: pinnedSeed})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	h, m, sec := plan.Today.Clock()
	if h != 0 || m != 0 || sec != 0 {
		t.Errorf("Today = %s, want midnight", plan.Today)
	}
	if name := plan.Today.Location().String(); name != model.DefaultTimeZone {
		t.Errorf("Today is in %s, want %s", name, model.DefaultTimeZone)
	}
}

// Build must refuse a plan it cannot stand behind. These are the two ways a
// scenario goes wrong that no amount of care prevents: citing evidence it did
// not plant, and reusing an id another scenario already took.
func TestBuildRejectsBrokenScenarios(t *testing.T) {
	day := mustDay(t, pinnedDay)

	unresolvable := func(s *Script) Trap {
		return Trap{
			ID: "broken-ref", Kind: StaleData, Description: "cites nothing that exists",
			MustSurface: true,
			Expect:      Expect{SignalKind: model.KindStaleness, Keywords: []string{"nothing"}},
			PlantedRefs: []model.Citation{{Source: model.SourceEmail, Ref: "m-never-planted"}},
		}
	}
	if _, err := build(Config{Today: day}, []Scenario{unresolvable}); err == nil {
		t.Error("Build accepted a trap citing an artifact nobody planted")
	} else if !strings.Contains(err.Error(), "broken-ref") {
		t.Errorf("error %q does not name the scenario", err)
	}

	duplicate := func(s *Script) Trap {
		ref := s.Email(model.Email{
			ID: "m-dup", ThreadID: "t-dup", TS: s.DayAt(-1, 9, 0),
			From: Marcus, Subject: "twice", Body: "twice",
		})
		s.Email(model.Email{
			ID: "m-dup", ThreadID: "t-dup", TS: s.DayAt(-1, 10, 0),
			From: Marcus, Subject: "twice", Body: "twice",
		})
		return Trap{
			ID: "dup-ids", Kind: StaleData, Description: "plants the same id twice",
			MustSurface: true,
			Expect:      Expect{SignalKind: model.KindStaleness, Keywords: []string{"twice"}},
			PlantedRefs: []model.Citation{ref},
		}
	}
	if _, err := build(Config{Today: day}, []Scenario{duplicate}); err == nil {
		t.Error("Build accepted two artifacts sharing one id")
	}

	invertedThread := func(s *Script) Trap {
		parent := s.Email(model.Email{
			ID: "m-parent", ThreadID: "t-inverted", TS: s.DayAt(-1, 18, 0),
			From: Marcus, Subject: "question", Body: "question",
		})
		s.Email(model.Email{
			ID: "m-reply", ThreadID: "t-inverted", TS: s.DayAt(-1, 9, 0),
			From: Avery, To: to(Marcus), Subject: "Re: question", Body: "answer",
			InReplyTo: "m-parent",
		})
		return Trap{
			ID: "inverted-thread", Kind: StaleData, Description: "answers a message sent later that day",
			MustSurface: true,
			Expect:      Expect{SignalKind: model.KindStaleness, Keywords: []string{"question"}},
			PlantedRefs: []model.Citation{parent},
		}
	}
	if _, err := build(Config{Today: day}, []Scenario{invertedThread}); err == nil {
		t.Error("Build accepted a reply that predates the message it answers")
	}

	strayDate := func(s *Script) Trap {
		ref := s.Email(model.Email{
			ID: "m-stray", ThreadID: "t-stray", TS: s.DayAt(-400, 9, 0),
			From: Marcus, Subject: "last year", Body: "last year",
		})
		return Trap{
			ID: "stray-date", Kind: StaleData, Description: "plants outside the corpus window",
			MustSurface: true,
			Expect:      Expect{SignalKind: model.KindStaleness, Keywords: []string{"last year"}},
			PlantedRefs: []model.Citation{ref},
		}
	}
	if _, err := build(Config{Today: day}, []Scenario{strayDate}); err == nil {
		t.Error("Build accepted an artifact dated outside the corpus window")
	}
}
