package datagen

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func mustGenerate(t *testing.T) *Plan {
	t.Helper()
	plan, err := Generate(Config{Seed: pinnedSeed, Today: mustDay(t, pinnedDay)})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return plan
}

// The whole regression suite and the committed golden digest rest on this one
// property, so it is asserted on the marshalled corpus rather than on a count.
func TestGenerateIsDeterministic(t *testing.T) {
	a, err := json.Marshal(mustGenerate(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(mustGenerate(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("two runs with the same seed and day produced different corpora")
	}
}

// The corpus is the size the SPEC asks for, and it is that size exactly: an
// approximate target would make every distribution assertion below approximate
// too.
func TestCorpusSize(t *testing.T) {
	plan := mustGenerate(t)
	if len(plan.Emails) != TargetEmails {
		t.Errorf("emails = %d, want %d", len(plan.Emails), TargetEmails)
	}
	if n := len(plan.Notes); n != 10 {
		t.Errorf("notes = %d, want 10 (SPEC §1)", n)
	}
	if n := len(plan.Tasks); n != 5 {
		t.Errorf("tasks = %d, want 5 (SPEC §1)", n)
	}
	if n := len(plan.Events); n < 60 {
		t.Errorf("events = %d, want a month of calendar rather than a handful", n)
	}
	for _, want := range []string{
		"notes/sprint.md", "notes/q2-planning.md", "notes/hiring-status.md",
		"notes/customer-veritas-renewal.md", "notes/board-update-cadence.md",
	} {
		if !hasNote(plan, want) {
			t.Errorf("no %s in the corpus", want)
		}
	}
}

// ~30% noise is what makes recall non-trivial (SPEC §1). The band is wide
// because the share is a property of a distribution, and narrow enough that a
// corpus of pure signal or pure spam fails.
func TestNoiseShare(t *testing.T) {
	plan := mustGenerate(t)
	byLabel := map[string]int{}
	for _, e := range plan.Emails {
		for _, l := range e.Labels {
			byLabel[l]++
		}
	}
	noise := noiseCount(plan.Emails)
	share := float64(noise) / float64(len(plan.Emails))
	if share < 0.25 || share > 0.35 {
		t.Errorf("noise share = %.1f%% (%d of %d), want roughly 30%%", share*100, noise, len(plan.Emails))
	}
	for _, l := range noiseLabels {
		if byLabel[l] == 0 {
			t.Errorf("no %s in the corpus; the noise is one flavour, not a mix", l)
		}
	}
}

// Mail thins out at the weekend and never stops. A corpus with silent days is
// one where "this thread went quiet" is measured against nothing.
func TestArrivalDistribution(t *testing.T) {
	plan := mustGenerate(t)
	checkEveryDayCarriesMail(t, plan)

	perDay := mailPerDay(plan)
	var business, weekend, businessDays, weekendDays int
	for i := range CorpusDays {
		day := plan.Today.AddDate(0, 0, -i)
		n := perDay[day.Format("2006-01-02")]
		if isBusinessDay(day) {
			business, businessDays = business+n, businessDays+1
			continue
		}
		weekend, weekendDays = weekend+n, weekendDays+1
	}
	if businessDays == 0 || weekendDays == 0 {
		t.Fatal("the corpus window contains no weekend or no working day")
	}
	perBusiness := float64(business) / float64(businessDays)
	perWeekend := float64(weekend) / float64(weekendDays)
	if perBusiness <= perWeekend {
		t.Errorf("mail per working day (%.1f) is not above mail per weekend day (%.1f)", perBusiness, perWeekend)
	}
}

// A corpus where two messages are the same message is a corpus an engine can
// learn the shape of instead of reading. Ids are unique by construction (the
// plan refuses duplicates); this is about what a reader would actually see.
func TestNoTwoMessagesReadTheSame(t *testing.T) {
	plan := mustGenerate(t)
	seen := make(map[string]string, len(plan.Emails))
	for _, e := range plan.Emails {
		key := e.Subject + "\x00" + e.Body
		if first, dup := seen[key]; dup {
			t.Fatalf("emails %s and %s are byte-identical:\n%s\n%s", first, e.ID, e.Subject, e.Body)
		}
		seen[key] = e.ID
	}
}

// The calendar has to carry the three shapes the SPEC names — meetings,
// declines, blocks — plus the standing Tue/Thu 9-11 deep-work block the profile
// asks to be defended.
func TestCalendarShape(t *testing.T) {
	plan := mustGenerate(t)

	var declined, cancelled, allDay, deepWork int
	for _, ev := range plan.Events {
		switch {
		case ev.Status == model.StatusCancelled:
			cancelled++
		case ev.DeclinedBy(Avery.Email):
			declined++
		}
		if ev.AllDay {
			allDay++
		}
		if !strings.HasPrefix(ev.UID, "ev-deep-work-") {
			continue
		}
		deepWork++
		switch ev.Start.Weekday() {
		case time.Tuesday, time.Thursday:
		default:
			t.Errorf("deep-work block %s is on a %s", ev.UID, ev.Start.Weekday())
		}
		if h, m, _ := ev.Start.Clock(); h != 9 || m != 0 {
			t.Errorf("deep-work block %s starts at %02d:%02d, want 09:00", ev.UID, h, m)
		}
		if h, m, _ := ev.End.Clock(); h != 11 || m != 0 {
			t.Errorf("deep-work block %s ends at %02d:%02d, want 11:00", ev.UID, h, m)
		}
	}
	if deepWork < 8 {
		t.Errorf("deep-work blocks = %d, want the standing block across the whole window", deepWork)
	}
	for name, n := range map[string]int{"declined": declined, "cancelled": cancelled, "all-day": allDay} {
		if n == 0 {
			t.Errorf("no %s events on the calendar", name)
		}
	}
}

// The seed moves the filler and never the exam — the same guarantee Build makes
// about the scenarios, restated over the whole corpus because the filler is the
// half that is actually sampled.
func TestSeedMovesFillerNotTraps(t *testing.T) {
	day := mustDay(t, pinnedDay)
	base, err := Generate(Config{Seed: pinnedSeed, Today: day})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	other, err := Generate(Config{Seed: pinnedSeed + 1, Today: day})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	a, _ := json.Marshal(base.Traps)
	b, _ := json.Marshal(other.Traps)
	if string(a) != string(b) {
		t.Error("the answer key moved with the seed")
	}
	if len(base.Emails) != len(other.Emails) {
		t.Errorf("corpus size moved with the seed: %d vs %d", len(base.Emails), len(other.Emails))
	}
	same := 0
	for i := range base.Emails {
		if base.Emails[i].Body == other.Emails[i].Body {
			same++
		}
	}
	if same == len(base.Emails) {
		t.Error("the seed changed nothing in the filler")
	}
}

// The filler's invariants are properties of the generator, not of one lucky
// draw, so they are checked across seeds and across anchor days that land on
// different weekdays and either side of a daylight-saving change.
//
// This is the test that would catch the failure mode the whole layer is built
// to avoid: filler that answers the exam by accident, on some seed nobody ran.
func TestFillerInvariantsHoldForAnySeedAndAnchor(t *testing.T) {
	for _, seed := range []int64{1, pinnedSeed, 2026} {
		for _, day := range []string{pinnedDay, "2026-09-02", "2026-12-24"} {
			t.Run(fmt.Sprintf("seed-%d-%s", seed, day), func(t *testing.T) {
				plan, err := Generate(Config{Seed: seed, Today: mustDay(t, day)})
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if len(plan.Emails) != TargetEmails {
					t.Errorf("emails = %d, want %d", len(plan.Emails), TargetEmails)
				}
				checkConversationsAreClosed(t, plan)
				checkNoFillerCollisions(t, plan)
				checkEveryDayCarriesMail(t, plan)
			})
		}
	}
}

// checkConversationsAreClosed holds the rule that keeps the filler from
// planting findings: a conversation is opened by the counterpart, has somebody
// answer it, and does not end on a question.
//
// Noise is exempt and has to be: a newsletter is a one-way message nobody
// replies to, which is exactly the shape being ruled out here. What keeps noise
// off the page is the profile's own suppression rules, and the negative traps
// are what prove those work.
func checkConversationsAreClosed(t *testing.T, plan *Plan) {
	t.Helper()

	threads := map[string][]model.Email{}
	var order []string
	for _, e := range plan.Emails {
		if !strings.HasPrefix(e.ThreadID, "f-t-") || isNoise(e) {
			continue
		}
		if _, seen := threads[e.ThreadID]; !seen {
			order = append(order, e.ThreadID)
		}
		threads[e.ThreadID] = append(threads[e.ThreadID], e)
	}
	if len(order) == 0 {
		t.Fatal("no filler conversations in the corpus")
	}

	for _, id := range order {
		msgs := threads[id]
		if len(msgs) < 2 {
			t.Errorf("filler conversation %s has one message; an unanswered first contact is an open thread", id)
		}
		if msgs[0].From.Email == Avery.Email {
			t.Errorf("filler conversation %s is opened by the owner; that reads as \"reached out, no answer\"", id)
		}
		if last := msgs[len(msgs)-1]; strings.Contains(last.Body, "?") {
			t.Errorf("filler conversation %s ends on a question (%s): %q", id, last.ID, last.Body)
		}
	}
}

// checkNoFillerCollisions asserts that every collision on the calendar was
// scripted by a scenario. A filler event that overlapped one would be an
// unscripted conflict finding — the calendar equivalent of an open thread.
func checkNoFillerCollisions(t *testing.T, plan *Plan) {
	t.Helper()

	var live []model.CalEvent
	for _, ev := range plan.Events {
		if ev.Status == model.StatusCancelled || ev.DeclinedBy(Avery.Email) || ev.AllDay {
			continue
		}
		live = append(live, ev)
	}
	for i := range live {
		for j := i + 1; j < len(live); j++ {
			a, b := live[i], live[j]
			if !a.Start.Before(b.End) || !b.Start.Before(a.End) {
				continue
			}
			if strings.HasPrefix(a.UID, "f-ev-") || strings.HasPrefix(b.UID, "f-ev-") {
				t.Errorf("filler event overlaps: %s (%s) and %s (%s)",
					a.UID, a.Start.Format(time.RFC3339), b.UID, b.Start.Format(time.RFC3339))
			}
		}
	}
}

// checkEveryDayCarriesMail asserts the corpus has no silent days.
func checkEveryDayCarriesMail(t *testing.T, plan *Plan) {
	t.Helper()
	perDay := mailPerDay(plan)
	for i := range CorpusDays {
		day := plan.Today.AddDate(0, 0, -i)
		if perDay[day.Format("2006-01-02")] == 0 {
			t.Errorf("no mail at all on %s", day.Format("2006-01-02 (Mon)"))
		}
	}
}

func mailPerDay(plan *Plan) map[string]int {
	perDay := map[string]int{}
	for _, e := range plan.Emails {
		perDay[e.TS.Format("2006-01-02")]++
	}
	return perDay
}

func hasNote(plan *Plan, path string) bool {
	for _, n := range plan.Notes {
		if n.Path == path {
			return true
		}
	}
	return false
}
