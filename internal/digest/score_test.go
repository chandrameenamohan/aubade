package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// sig builds a minimal valid signal for a ranking test.
func sig(id string, p model.Priority, deadline *time.Time, cites ...model.Citation) model.Signal {
	if len(cites) == 0 {
		cites = []model.Citation{{Source: model.SourceTask, Ref: "t-1"}}
	}
	return model.Signal{
		ID: id, Kind: model.KindCommitments, Priority: p, Title: id,
		Citations: cites, SectionHint: model.SectionUrgentToday,
		Confidence: model.Certain, Deadline: deadline,
	}
}

// emptyIndex is an index over nothing: citations resolve to no timestamp, so
// the recency axis contributes zero and a test can isolate the other two.
func emptyIndex(t *testing.T) *index {
	t.Helper()
	return newIndex(&model.Corpus{}, model.Location())
}

func at(t *testing.T, v string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("bad timestamp %q: %v", v, err)
	}
	return parsed
}

// The weights are a policy, so they get asserted as one. Priority dominates,
// but not so completely that a deadline cannot matter: a P0 always outranks a
// P2 however late the P2 is, and an overdue P1 outranks a P0 with nothing due.
func TestPriorityAndDeadlineTradeOff(t *testing.T) {
	loc := model.Location()
	now := at(t, "2026-08-31T06:00:00-07:00").In(loc)
	day := startOfDay(now)
	idx := emptyIndex(t)

	longOverdue := at(t, "2026-07-01T09:00:00-07:00")
	freshP1 := at(t, "2026-08-28T17:00:00-07:00")

	p0Nothing := scoreOf(sig("a", model.P0, nil), idx, now, day, loc)
	p2Overdue := scoreOf(sig("b", model.P2, &longOverdue), idx, now, day, loc)
	p1Overdue := scoreOf(sig("c", model.P1, &freshP1), idx, now, day, loc)

	if p2Overdue.Total >= p0Nothing.Total {
		t.Errorf("a very overdue P2 (%d) should not outrank a P0 (%d): the profile decides who matters",
			p2Overdue.Total, p0Nothing.Total)
	}
	if p1Overdue.Total <= p0Nothing.Total {
		t.Errorf("an overdue P1 (%d) should outrank a P0 with nothing due (%d): the deadline decides what is on fire",
			p1Overdue.Total, p0Nothing.Total)
	}
}

// Overdue-ness accrues, but only to a cap. Something six weeks late is a
// different conversation from something a week late, not six times as urgent.
func TestOverdueBonusIsCapped(t *testing.T) {
	loc := model.Location()
	now := at(t, "2026-08-31T06:00:00-07:00").In(loc)
	day := startOfDay(now)

	week := at(t, "2026-08-24T09:00:00-07:00")
	quarter := at(t, "2026-06-01T09:00:00-07:00")

	weekLate, _ := deadlineWeight(week, now, day, loc)
	quarterLate, _ := deadlineWeight(quarter, now, day, loc)

	if quarterLate != weekLate {
		t.Errorf("overdue bonus should cap at %d days: 7 days = %d, 90 days = %d",
			deadlineOverdueCap, weekLate, quarterLate)
	}
	if maxOverdue := deadlineOverdue + deadlineOverdueCap*deadlineOverdueDay; weekLate != maxOverdue {
		t.Errorf("a week overdue scores %d, want the cap %d", weekLate, maxOverdue)
	}
}

// Recency is the lightest axis: fresh evidence breaks ties, it does not promote
// a line past a live deadline.
func TestRecencyOnlyBreaksTies(t *testing.T) {
	loc := model.Location()
	now := at(t, "2026-08-31T06:00:00-07:00").In(loc)
	day := startOfDay(now)

	fresh, _ := recencyWeight(at(t, "2026-08-31T02:00:00-07:00"), now)
	old, _ := recencyWeight(at(t, "2026-06-01T02:00:00-07:00"), now)

	if fresh != recencyToday || old != 0 {
		t.Fatalf("recency weights: fresh=%d old=%d", fresh, old)
	}
	dueToday, _ := deadlineWeight(now.Add(4*time.Hour), now, day, loc)
	if fresh >= dueToday {
		t.Errorf("recency (%d) must not outweigh a deadline due today (%d)", fresh, dueToday)
	}
}

// The order is total: two signals that agree on every weighted axis still have
// exactly one correct order, because the last key is the unique signal id.
func TestRankIsTotalAndStable(t *testing.T) {
	loc := model.Location()
	now := at(t, "2026-08-31T06:00:00-07:00").In(loc)
	day := startOfDay(now)
	idx := emptyIndex(t)

	ss := model.Signals{
		sig("commitments:z", model.P1, nil),
		sig("commitments:a", model.P1, nil),
		sig("commitments:m", model.P0, nil),
	}
	got := rank(ss, idx, now, day, loc)

	want := []string{"commitments:m", "commitments:a", "commitments:z"}
	for i, id := range want {
		if got[i].Signal.ID != id {
			t.Errorf("rank[%d] = %s, want %s", i, got[i].Signal.ID, id)
		}
	}
}

// Every score explains itself. A ranking nobody can read is a ranking nobody
// can argue with, which is how a weight nobody agrees with survives.
func TestScoreExplainsItself(t *testing.T) {
	loc := model.Location()
	now := at(t, "2026-08-31T06:00:00-07:00").In(loc)
	overdue := at(t, "2026-08-28T17:00:00-07:00")

	sc := scoreOf(sig("x", model.P1, &overdue), emptyIndex(t), now, startOfDay(now), loc)
	if sc.Why == "" {
		t.Fatal("a score with no explanation")
	}
	for _, want := range []string{"P1", "overdue"} {
		if !strings.Contains(sc.Why, want) {
			t.Errorf("Why = %q, missing %q", sc.Why, want)
		}
	}
}
