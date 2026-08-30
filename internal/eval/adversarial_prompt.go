package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The question the adversarial suite asks, and the shape the answer has to
// take.
//
// The schema is built from the same vocabularies the graders use —
// datagen.KnownCategories and model.KnownKinds — rather than being written out
// by hand. A schema that lists a category the answer key has since renamed
// would put the model on the wrong side of a rule it was never shown, and the
// rejection would be ours rather than its.
//
// The prompt is written to EVAL-PRINCIPLES #20: a critic told to confirm
// confirms. It is shown what is already caught, precisely so it does not write
// a fourth version of it, and it is told in as many words that a trap the
// engine sails through is a wasted question.

// adversarialSchema is the JSON Schema one authoring answer must obey.
var adversarialSchema = buildAdversarialSchema()

func buildAdversarialSchema() string {
	person := `{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"}},"required":["name","email"],"additionalProperties":false}`
	stamp := `{"type":"object","properties":{"day_offset":{"type":"integer"},"hour":{"type":"integer"},"minute":{"type":"integer"}},"required":["day_offset","hour","minute"],"additionalProperties":false}`
	stringList := `{"type":"array","items":{"type":"string"}}`

	email := fmt.Sprintf(`{"type":"object","properties":{
"id":{"type":"string"},"thread_id":{"type":"string"},
"day_offset":{"type":"integer"},"hour":{"type":"integer"},"minute":{"type":"integer"},
"from":%[1]s,"to":{"type":"array","items":%[1]s},
"subject":{"type":"string"},"body":{"type":"string"},
"in_reply_to":{"type":"string"},"labels":%[2]s},
"required":["id","thread_id","day_offset","hour","minute","from","to","subject","body"],
"additionalProperties":false}`, person, stringList)

	attendee := fmt.Sprintf(`{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"},"partstat":{"type":"string","enum":%s}},"required":["name","email","partstat"],"additionalProperties":false}`,
		jsonEnum(partStats()))

	event := fmt.Sprintf(`{"type":"object","properties":{
"uid":{"type":"string"},"summary":{"type":"string"},"description":{"type":"string"},"location":{"type":"string"},
"day_offset":{"type":"integer"},"start_hour":{"type":"integer"},"start_minute":{"type":"integer"},
"duration_minutes":{"type":"integer"},
"status":{"type":"string","enum":["CONFIRMED","TENTATIVE","CANCELLED"]},
"organizer":%s,"attendees":{"type":"array","items":%s},"created":%s},
"required":["uid","summary","day_offset","start_hour","start_minute","duration_minutes","status","organizer","attendees"],
"additionalProperties":false}`, person, attendee, stamp)

	note := fmt.Sprintf(`{"type":"object","properties":{
"path":{"type":"string"},"title":{"type":"string"},"day_offset":{"type":"integer"},
"body":{"type":"string"},"attendees":%[1]s,"tags":%[1]s},
"required":["path","title","day_offset","body"],
"additionalProperties":false}`, stringList)

	task := `{"type":"object","properties":{
"id":{"type":"string"},"title":{"type":"string"},"done":{"type":"boolean"},
"owner":{"type":"string"},"due_day_offset":{"type":"integer"}},
"required":["id","title","done"],
"additionalProperties":false}`

	scenario := fmt.Sprintf(`{"type":"object","properties":{
"id":{"type":"string"},
"kind":{"type":"string","enum":%s},
"description":{"type":"string"},
"must_surface":{"type":"boolean"},
"expect":{"type":"object","properties":{"signal_kind":{"type":"string","enum":%s},"keywords":%s},"required":["signal_kind","keywords"],"additionalProperties":false},
"emails":{"type":"array","items":%s},
"events":{"type":"array","items":%s},
"notes":{"type":"array","items":%s},
"tasks":{"type":"array","items":%s}},
"required":["id","kind","description","must_surface","expect","emails","events","notes","tasks"],
"additionalProperties":false}`,
		jsonEnum(datagen.KnownCategories), jsonEnum(model.KnownKinds), stringList,
		email, event, note, task)

	schema := fmt.Sprintf(`{"type":"object","properties":{"scenarios":{"type":"array","items":%s}},"required":["scenarios"],"additionalProperties":false}`, scenario)
	return compactJSON(schema)
}

// partStats is the RSVP vocabulary, as the schema's enum.
func partStats() []string {
	return []string{
		string(model.PartStatNeedsAction), string(model.PartStatAccepted),
		string(model.PartStatDeclined), string(model.PartStatTentative),
		string(model.PartStatDelegated),
	}
}

func jsonEnum(values []string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]" // unreachable: []string always marshals
	}
	return string(raw)
}

// compactJSON strips the whitespace the literals above use for readability. The
// schema travels as a command-line argument, and a newline in an argv entry is
// a good way to find out how a shell quotes things.
func compactJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		// A malformed schema literal is a programming error; returning the
		// unexpanded text lets the runner reject it with the text in hand.
		return s
	}
	return buf.String()
}

// authorPrompt is the whole question. feedback is empty on the first attempt
// and carries the rejected scenarios on the retry.
func authorPrompt(in AdversarialInput, want int, feedback []Rejection) string {
	var b strings.Builder

	b.WriteString(`You are writing NEW test cases for an eval, and your job is adversarial.

The system under test is "aubade": it reads one person's email, calendar,
meeting notes, task list and profile, and writes her a one-page morning digest.
It is graded by planted traps — situations deliberately put in the corpus, each
with an answer-key entry saying whether the digest must surface it or must leave
it alone.

Every trap it is graded on today was written by the people who wrote the
engine, which is the weakness you are here to exploit. Assume the engine has
blind spots. Find them. A scenario the engine sails through is a wasted
question; a scenario that is unfair — evidence nobody could reasonably read the
way you did — is worse, because the miss it produces names nothing to fix.

Write situations that are realistic, specific and hard: the kind of thing that
actually happens in a founder's inbox and that a naive keyword matcher would get
wrong. Obligations phrased as courtesies. A commitment made and then quietly
renegotiated three messages later. A conflict that is only a conflict once you
read the timezone. Something that looks urgent and is not.

`)

	fmt.Fprintf(&b, "Anchor date (\"today\"): %s (%s). Every date you write is a day_offset from it:\n",
		in.Today.Format("2006-01-02"), in.Today.Format("Monday"))
	b.WriteString("0 is today, -1 is yesterday, +3 is three days out. Email and notes must be\n")
	fmt.Fprintf(&b, "dated in the past (day_offset between -%d and 0); calendar events may reach\n", datagen.CorpusDays)
	fmt.Fprintf(&b, "forward as far as +%d.\n\n", datagen.LookaheadDays)

	writeOwner(&b, in.Corpus)
	writeExtractors(&b)
	writeCatalog(&b, in.Traps)

	b.WriteString(`Rules your answer is validated against — a scenario that breaks one is thrown
out, so read them:

  * "id" is a lowercase slug and must not collide with an existing trap id.
    Every email id, event uid, note path and task id must be new too.
  * "kind" is the situation's category and "expect.signal_kind" is the extractor
    that must answer it. They are different vocabularies; both are closed.
  * "must_surface" true means the digest MUST report it; false means the digest
    must leave it alone. Write some of each — an eval with only positive cases
    teaches an engine to say yes to everything.
  * "expect.keywords" must each appear VERBATIM in the text your own scenario
    plants (a subject, a body, an event summary, a note). They are how a grader
    finds your finding in the prose, so pick distinctive phrases, not "meeting".
  * Every scenario must plant at least one artifact. You do not write citations:
    they are derived from what you planted, so a trap cannot cite evidence it
    did not write.
  * Mail is addressed to the owner above unless the scenario is specifically
    about mail that is not.
  * A reply ("in_reply_to") must be dated after the message it answers.

`)

	if len(feedback) > 0 {
		b.WriteString("Your previous answer was REJECTED. Here is every rule each scenario broke:\n\n")
		for _, r := range feedback {
			fmt.Fprintf(&b, "  - %s: %s\n", r.ID, clip(r.Reason, 600))
		}
		fmt.Fprintf(&b, "\nWrite %d replacement scenario(s) that fix all of that. This is the last attempt.\n", want)
		return b.String()
	}

	fmt.Fprintf(&b, "Write exactly %d scenarios, none of them a variation on one already in the\ncatalog above.\n", want)
	return b.String()
}

// writeOwner tells the model whose morning this is, and which of her own rules
// the suppression half of the exam is graded against. The suppressions are
// quoted verbatim with their line numbers: a negative trap invented against a
// paraphrase of her rules would be graded against the real ones.
func writeOwner(b *strings.Builder, corpus *model.Corpus) {
	if corpus == nil || corpus.Profile == nil {
		return
	}
	p := corpus.Profile
	fmt.Fprintf(b, "The owner of the digest: %s.\n\n", p.Owner)

	if len(p.People) > 0 {
		b.WriteString("People who matter to her, and how much:\n")
		for _, person := range p.People {
			fmt.Fprintf(b, "  - %s", person.Name)
			if person.Role != "" {
				fmt.Fprintf(b, ", %s", person.Role)
			}
			fmt.Fprintf(b, " (%s)\n", person.Priority)
		}
		b.WriteString("\n")
	}
	if len(p.Suppressions) > 0 {
		b.WriteString("Her own rules about what must never be surfaced — the negative half of the\nexam is graded against these, verbatim:\n")
		for _, r := range p.Suppressions {
			fmt.Fprintf(b, "  - %s\n", r.Text)
		}
		b.WriteString("\n")
	}
}

// writeExtractors names the components a trap can be hung on, in one line each.
// The model has to pick one, and picking one it does not understand is how a
// task ends up graded against the wrong component.
func writeExtractors(b *strings.Builder) {
	b.WriteString("The extractors, one of which your trap must name in expect.signal_kind:\n")
	for _, k := range model.KnownKinds {
		fmt.Fprintf(b, "  - %-15s %s\n", k, extractorPurpose(k))
	}
	b.WriteString("\n")
}

func extractorPurpose(kind string) string {
	switch kind {
	case model.KindCommitments:
		return "promises the owner made and has not kept, with their deadlines"
	case model.KindQuietThreads:
		return "threads that have gone quiet on her side and matter to someone"
	case model.KindConflicts:
		return "calendar overlaps, double-bookings, blocks someone booked over"
	case model.KindContradictions:
		return "two sources that cannot both be true; both sides get cited"
	case model.KindDispatchables:
		return "short asks she could answer in two lines and has not"
	case model.KindSuppressions:
		return "items held back on her own profile rules, and the record of it"
	case model.KindStaleness:
		return "sources too old or too undated to be relied on today"
	default:
		return ""
	}
}

// writeCatalog shows what is already asked, so the model writes something else.
func writeCatalog(b *strings.Builder, traps datagen.Traps) {
	if len(traps) == 0 {
		return
	}
	b.WriteString("Traps ALREADY in the exam. Do not write another version of any of these:\n")
	for _, t := range traps {
		must := "must NOT surface"
		if t.MustSurface {
			must = "must surface"
		}
		fmt.Fprintf(b, "  - %s [%s, %s] %s\n", t.ID, t.Kind, must, clip(t.Description, 140))
	}
	b.WriteString("\n")
}
