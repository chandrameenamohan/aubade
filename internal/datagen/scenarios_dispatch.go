package datagen

import (
	"fmt"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// dispatchableHalberdYesNo is the canonical one-minute reply: a reference
// customer asking a yes/no question whose answer is already sitting in another
// thread.
//
// Both halves are planted. Renee's question is P1 and gets a same-day reply
// "period"; Jordan's status mail three days earlier is what makes the answer
// yes. A digest that surfaces the question without the answer has found an
// urgent item, not a dispatchable — the trap grades whether the engine joined
// the two threads.
func dispatchableHalberdYesNo(s *Script) Trap {
	rollout := s.NextWeekday(time.Friday)

	engineering := s.Email(model.Email{
		ID: "m-halberd-01", ThreadID: "t-halberd-eng", TS: s.DayAt(-3, 17, 20),
		From: Jordan, To: to(Avery), CC: to(Tomas),
		Subject: "Halberd rollout — engineering status",
		Body: fmt.Sprintf("We're on track for the %s rollout. Two minor regressions opened by QA "+
			"yesterday, neither blocking.\n\nJordan", rollout.Format("Jan 2")),
		Labels: []string{"internal"},
	})
	question := s.Email(model.Email{
		ID: "m-halberd-02", ThreadID: "t-halberd-rollout", TS: s.DayAt(-2, 14, 8),
		From:    Renee,
		Subject: fmt.Sprintf("is the %s rollout still on?", rollout.Format("Jan 2")),
		Body: "Avery — our internal stakeholder meeting is tomorrow morning and I need to tell " +
			"them yes or no. That's the whole question.\n\nRenee",
		Labels: []string{"customer"},
	})

	return Trap{
		ID:   "dispatchable-halberd-yes-no",
		Kind: QuickReply,
		Description: "Renee Tan at Halberd needs a yes or no on the rollout before her stakeholder " +
			"meeting tomorrow, and Jordan's status mail three days earlier already says " +
			"engineering is on track. One sentence, in Avery's voice, ready to send.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindDispatchables,
			Keywords:   []string{"Halberd", "rollout"},
		},
		PlantedRefs: []model.Citation{question, engineering},
	}
}

// dispatchableExpenseApprovals is the small thing that is easy to drop: three
// approvals, one click each, blocking somebody else's money.
//
// It is planted as a second dispatchable because the first one needs judgment
// (join two threads, decide the answer is yes) and this one needs none. If only
// the hard case existed, a scorecard could not distinguish an engine that finds
// dispatchables from an engine that got lucky on a single thread.
func dispatchableExpenseApprovals(s *Script) Trap {
	waiting := s.Email(model.Email{
		ID: "m-expenses-01", ThreadID: "t-expenses", TS: s.DayAt(-4, 10, 4),
		From:    Nadia,
		Subject: "three expense reports need your approval",
		Body: "Avery — three expense reports have been sitting in the queue since Monday. " +
			"They block this month's team payouts. One click each.\n\nNadia",
		Labels: []string{"internal", "finance"},
	})

	return Trap{
		ID:   "dispatchable-expense-approvals",
		Kind: QuickReply,
		Description: "Three expense reports are waiting on Avery's approval and are blocking team " +
			"payouts. No judgement required, seconds to clear, and exactly the kind of thing that " +
			"stays in the inbox for a fortnight.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindDispatchables,
			Keywords:   []string{"expense report", "approval"},
		},
		PlantedRefs: []model.Citation{waiting},
	}
}

// recruiterPatternKestrel is the only trap where the right answer is "suppress
// these, and say so".
//
// profile.md: recruiters cold-emailing are P4 and must not be surfaced — unless
// three or more arrive from the same firm in a week, "then surface as a
// pattern, not as individual threads". So the suppression extractor has to
// count before it drops, and both halves of that are graded: the pattern must
// appear, and the three threads must not appear individually.
//
// The seed picks each recruiter's opener; the signature that carries the firm
// name and the word every extractor keys on is fixed, because the trap must not
// depend on which sentence the seed chose.
func recruiterPatternKestrel(s *Script) Trap {
	type outreach struct {
		id     string
		from   model.Person
		day    int
		hour   int
		minute int
		subj   string
	}
	outreaches := []outreach{
		{"m-kestrel-01", KestrelTalia, -6, 8, 30, "senior backend engineers, Bay Area"},
		{"m-kestrel-02", KestrelOwen, -4, 11, 15, "quick question about your eng hiring"},
		{"m-kestrel-03", KestrelRao, -1, 9, 45, "design leadership candidates"},
	}

	refs := make([]model.Citation, 0, len(outreaches))
	for _, o := range outreaches {
		opener := s.Pick(
			"Hope you don't mind the cold note.",
			"Saw the Tessera hiring page and thought I'd reach out.",
			"Following up on my last email — happy to be told no.",
		)
		refs = append(refs, s.Email(model.Email{
			ID: o.id, ThreadID: "t-" + o.id, TS: s.DayAt(o.day, o.hour, o.minute),
			From:    o.from,
			Subject: o.subj,
			Body: fmt.Sprintf("%s We place engineering and design talent at Series A companies and "+
				"have candidates who would fit Tessera.\n\n%s\nRecruiter, Kestrel Search",
				opener, o.from.Name),
			Labels: []string{"recruiter"},
		}))
	}

	return Trap{
		ID:   "recruiter-pattern-kestrel",
		Kind: RecruiterPattern,
		Description: "Three different recruiters at Kestrel Search cold-emailed Avery inside a week. " +
			"Individually each is P4 and suppressed; three from one firm in a week is the pattern " +
			"profile.md asks to be told about — as a pattern, never as three threads.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindSuppressions,
			Keywords:   []string{"Kestrel Search", "recruiter"},
		},
		PlantedRefs: refs,
	}
}
