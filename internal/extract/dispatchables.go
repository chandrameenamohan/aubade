package extract

import (
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Dispatchables answer the third of the digest's three questions: what can be
// handled right now, in one reply, without opening anything else.
//
// The test is deliberately about the *shape* of the ask, not about how
// important it is. An item qualifies when all of these hold:
//
//   - somebody is waiting on the owner (the thread's last word is theirs),
//   - the ask is short — a body under a few hundred characters, not a document
//     to review,
//   - it is answerable from what the owner already knows: a yes/no, a time, a
//     confirmation, an approval,
//   - and it is recent enough that a reply is still the right move.
//
// Anything the profile suppressed is out, which is what keeps this section from
// filling with newsletters that end in a question mark.
//
// Tasks join the same section when they are themselves one-reply items — a
// task that begins "Reply to…" or "Confirm…" is a dispatchable that happens to
// live on a list rather than in a thread.

// dispatchMaxBody is the length above which an ask stops being a one-liner.
const dispatchMaxBody = 700

// dispatchMaxAgeBusinessDays is how stale an ask can be and still be worth
// answering rather than escalating. Past this, quiet-threads owns it.
const dispatchMaxAgeBusinessDays = 5

// smallAskPhrases are the asks a single reply closes.
var smallAskPhrases = []string{
	"yes or no", "can you confirm", "please confirm", "confirm that", "is it still",
	"is the", "are we still", "still on", "can we move", "does that work",
	"what time", "which day", "who should", "can you approve", "approve the",
	"sign off", "ok to", "okay to", "go ahead", "quick question", "one question",
	"let me know if", "let me know when", "let me know by", "need a yes",
	"need your ok", "need a decision", "can you send", "could you send",
	"any chance", "are you free", "does tuesday", "does wednesday",
	// Approvals are the smallest ask there is and they almost never arrive as
	// a question: "three expense reports need your approval" is a sentence, and
	// the reply that closes it is a click. A dispatchables extractor that only
	// reads question marks finds none of them, which is exactly the class of
	// item profile.md complains about losing.
	"need your approval", "needs your approval", "need approval", "needs approval",
	"waiting on your approval", "waiting for your approval", "approval needed",
	"need your sign-off", "needs your sign-off", "please approve", "one click",
}

// dispatchTaskVerbs open a task that is itself one reply.
var dispatchTaskVerbs = []string{"reply", "respond", "answer", "confirm", "ping", "email", "call back", "rsvp"}

// Dispatchables reports items that one reply closes.
func (t *Toolbox) Dispatchables() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	for _, th := range t.threads {
		if s, ok := t.dispatchableThread(g, th); ok {
			out = append(out, s)
		}
	}
	for i := range t.corpus.Tasks {
		if s, ok := t.dispatchableTask(g, &t.corpus.Tasks[i]); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// dispatchableThread decides whether a thread's open ask is a one-reply item.
func (t *Toolbox) dispatchableThread(g *ids, th *Thread) (model.Signal, bool) {
	if len(th.Messages) == 0 || !th.AwaitingOwner {
		return model.Signal{}, false
	}
	last := th.Last()
	if last.TS.After(t.now) {
		return model.Signal{}, false
	}
	if _, suppressed := t.supp.email(last.ID); suppressed {
		return model.Signal{}, false
	}
	if _, suppressed := t.supp.thread(th.ID); suppressed {
		return model.Signal{}, false
	}
	if len(last.Body) > dispatchMaxBody {
		return model.Signal{}, false
	}
	age := businessDaysBetween(last.TS, t.now, t.loc)
	if age > dispatchMaxAgeBusinessDays {
		return model.Signal{}, false
	}

	question := firstQuestion(last.Body)
	if question == "" {
		question = firstQuestion(last.Subject)
	}
	if question == "" || !containsAny(last.Body+" "+last.Subject, smallAskPhrases) {
		return model.Signal{}, false
	}

	match := t.prio.Of(last.From, last.Labels)
	section := model.SectionUrgentToday
	if containsAny(last.Body+" "+last.Subject, decisionPhrases) {
		section = model.SectionDecisions
	}

	who := last.From.Name
	if who == "" {
		who = last.From.Email
	}
	return model.Signal{
		ID:       g.next(model.KindDispatchables, last.ID),
		Kind:     model.KindDispatchables,
		Priority: match.Priority,
		Title:    fmt.Sprintf("one reply closes %s — %s", quote(truncate(th.Subject, 50)), who),
		Detail: fmt.Sprintf("%s asked %s (%s ago). %s.",
			who, quote(question), businessDayPhrase(age), match.Cited(t.profileRef())),
		Citations:   []model.Citation{emailCite(last.ID)},
		SectionHint: section,
		Confidence:  model.Certain,
	}, true
}

// dispatchableTask decides whether an open task is itself one reply.
func (t *Toolbox) dispatchableTask(g *ids, task *model.Task) (model.Signal, bool) {
	if task.Done {
		return model.Signal{}, false
	}
	first := strings.ToLower(firstWord(task.Title))
	matched := false
	for _, v := range dispatchTaskVerbs {
		if first == v || strings.HasPrefix(strings.ToLower(task.Title), v+" ") {
			matched = true
			break
		}
	}
	if !matched {
		return model.Signal{}, false
	}
	if task.HasDue() && task.Due.After(t.day.AddDate(0, 0, 1)) {
		return model.Signal{}, false
	}

	_, match := t.personInText(task.Title)
	section := model.SectionUrgentToday
	if containsAny(task.Title, decisionPhrases) {
		section = model.SectionDecisions
	}

	detail := fmt.Sprintf("On the list at %s:%d and still open.", sourcePaths[string(model.SourceTask)], task.Line)
	sig := model.Signal{
		ID:          g.next(model.KindDispatchables, "task", task.ID),
		Kind:        model.KindDispatchables,
		Priority:    match.Priority,
		Title:       "one reply closes it — " + truncate(task.Title, 70),
		Detail:      detail,
		Citations:   []model.Citation{taskCite(task.ID)},
		SectionHint: section,
		Confidence:  model.Certain,
	}
	if task.HasDue() {
		sig.Deadline = timePtr(task.Due)
		sig.Detail += " " + capitalize(t.deadlinePhrase(task.Due)) + "."
		if task.Due.Before(t.now) {
			sig.Priority = atMost(sig.Priority, model.P1)
		}
	}
	return sig, true
}

// firstWord returns the first whitespace-delimited word of s.
func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `.,;:!?"'`)
}
