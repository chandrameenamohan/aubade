package agentic

import (
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// One small corpus and one small fact base, shared by every test in this
// package. They are hand-written rather than generated: what is under test here
// is the orchestration layer, and a fixture whose contents a reader can hold in
// their head is what makes a failure legible.

var (
	testOwner = model.Person{Name: "Avery Chen", Email: "avery@tessera.example"}
	marcus    = model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.example"}
)

func testDay() time.Time {
	return time.Date(2026, time.August, 30, 6, 0, 0, 0, model.Location())
}

func testCorpus() *model.Corpus {
	day := testDay()
	return &model.Corpus{
		Source: "test",
		Emails: []model.Email{{
			ID:       "e-001",
			ThreadID: "t-1",
			TS:       day.Add(-3 * time.Hour),
			From:     marcus,
			To:       []model.Person{testOwner},
			Subject:  "Term sheet — we need an answer today",
			Body:     "The term sheet expires at 17:00 today. Can you confirm?",
		}},
		Events: []model.CalEvent{{
			UID:       "evt-7",
			Summary:   "Board sync",
			Start:     day.Add(4 * time.Hour),
			End:       day.Add(5 * time.Hour),
			Status:    model.StatusConfirmed,
			Organizer: testOwner,
			Created:   day.Add(-24 * time.Hour),
		}},
		Notes: []model.Note{{
			Path:  "notes/kickoff.md",
			Title: "Northstar kickoff",
			Date:  day.Add(-48 * time.Hour),
			Body:  "Owner to send the migration plan by Friday.",
		}},
		Profile: &model.Profile{
			Path:      "profile.md",
			Owner:     testOwner,
			ToneRules: []model.Rule{{Text: "short, lowercase greetings", Line: 112}},
			People:    []model.ProfilePerson{{Name: "Marcus Webb", Role: "lead investor", Priority: model.P0, Line: 20}},
		},
	}
}

// testSignals is the fact base: three signals over three distinct citations,
// one of which the toolbox could not rank.
func testSignals() model.Signals {
	return model.Signals{{
		ID:          "commitments:e-001",
		Kind:        model.KindCommitments,
		Priority:    model.P0,
		Title:       "Answer Marcus about the term sheet",
		Detail:      "Marcus asked for a decision by 17:00 today and has had no reply.",
		Citations:   []model.Citation{{Source: model.SourceEmail, Ref: "e-001"}},
		SectionHint: model.SectionUrgentToday,
		Confidence:  model.Certain,
	}, {
		ID:          "conflicts:evt-7",
		Kind:        model.KindConflicts,
		Priority:    model.P1,
		Title:       "Board sync needs an agenda",
		Detail:      "The board sync at 10:00 has no agenda attached.",
		Citations:   []model.Citation{{Source: model.SourceCalendar, Ref: "evt-7"}},
		SectionHint: model.SectionDecisions,
		Confidence:  model.Certain,
	}, {
		ID:          "quiet-threads:kickoff",
		Kind:        model.KindQuietThreads,
		Priority:    model.P2,
		Title:       "The Northstar migration plan has gone quiet",
		Detail:      "The kickoff note promised a plan by Friday and nothing followed.",
		Citations:   []model.Citation{{Source: model.SourceNote, Ref: "notes/kickoff.md"}},
		SectionHint: model.SectionUrgentToday,
		Confidence:  model.Unsure,
	}}
}

// testInput is a run wired to the given runners, with the fixture corpus.
func testInput(orch runner.Runner, voters ...runner.Runner) Input {
	if len(voters) == 0 && orch != nil {
		voters = []runner.Runner{orch}
	}
	return Input{
		Corpus:       testCorpus(),
		Signals:      testSignals(),
		Now:          testDay(),
		Loc:          model.Location(),
		Owner:        testOwner,
		Day:          "Sunday, August 30, 2026",
		Today:        "2026-08-30",
		ToolBin:      "/opt/aubade/bin/aubade",
		DataDir:      "/tmp/corpus",
		WorkDir:      "/tmp/work",
		Orchestrator: orch,
		Voters:       voters,
		Consensus:    true,
	}
}

// composer is a fake orchestrator that returns the given page.
func composer(t *testing.T, page string) *runnertest.Runner {
	t.Helper()
	return &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: page, ToolCalls: 4}
}

// goodPage is a composed page whose every citation is in the fact base.
const goodPage = `# Daily Digest — Sunday, August 30, 2026

## If there is one thing you must do right now:
**Answer Marcus about the term sheet.** It expires at 17:00 today. [email:e-001]

## Urgent To-Do Today
- **Send the board sync agenda.** [calendar:evt-7]

## Team & Product Pulse
- **Northstar's migration plan is still outstanding.** [note:notes/kickoff.md]
`
