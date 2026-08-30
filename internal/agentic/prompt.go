package agentic

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/styles"
)

// The orchestration prompt is a contract, so it is built here in one place
// rather than assembled from fragments at the call site.
//
// Its shape encodes the keystone decision (HLD §2). The model is handed the
// whole cited fact base and the toolbox, and it decides what to chase and how to
// arrange the page — but every clause about *what may be said* is a constraint,
// not a request, and each one is enforced somewhere else in the code as well:
//
//   - "cite in this exact form" is enforced by the post-compose validator, which
//     rejects the page if any ref is not in the fact base;
//   - "do not write the honesty sections" is enforced by appending them
//     regardless of what the model wrote;
//   - "you may run this one command" is enforced by the runner's allowlist.
//
// A prompt rule with no enforcement behind it is a wish. These three have
// enforcement behind them, which is why the prompt can afford to be short about
// them.

// customizeHeading is the heading the user's own prompt is placed under. It is
// named in the digest so a reader can tell a customized page from a default one.
const customizeHeading = "THE FORMAT THE USER ASKED FOR"

// LoadCustomize reads a --customize prompt file.
//
// An unreadable or empty file is an error rather than a silent fall-through to
// the default format: the user asked for a different page, and quietly giving
// them the standard one is the substitution this product exists not to make.
func LoadCustomize(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read --customize prompt %s: %w", path, err)
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "", fmt.Errorf("--customize prompt %s is empty; it has nothing to ask for", path)
	}
	return body, nil
}

// defaultFormat is the section contract from the sample digest — the page's
// shape when the user has not asked for another one.
const defaultFormat = `THE PAGE

Write one page of markdown, in this order:

  # Daily Digest — <weekday, month day, year>
  ## If there is one thing you must do right now:
      One paragraph. The single most important thing, and why it is that thing.
  ## Urgent To-Do Today
      Up to 6 bullets, most urgent first.
  ## Decisions & Approvals Needed
      Up to 5 bullets: someone is blocked on a decision from Avery.
  ## Team & Product Pulse
      Up to 5 bullets: what moved, what is at risk.
  ## Calendar & Personal
      Today's events in clock order, then any collisions between them.

Every bullet opens with a bold lead sentence, then the detail, then its
citations. A section with nothing in it still gets its heading and one honest
sentence saying so — "nothing needs you today" is an answer, a missing heading
is not.

For anything that can be closed with one reply, draft that reply in a fenced
` + "```text" + ` block. Never invent the answer itself: draft the shape and leave
the fact to Avery.`

// PromptInput is everything the orchestration prompt is built from.
type PromptInput struct {
	Day       string
	Owner     model.Person
	Signals   model.Signals
	Profile   *model.Profile
	ToolBin   string
	DataDir   string
	Today     string
	MaxCalls  int
	Customize string
	Decisions []Decision
}

// BuildPrompt composes the orchestration prompt.
func BuildPrompt(in PromptInput) (string, error) {
	facts, err := json.MarshalIndent(in.Signals, "", "  ")
	if err != nil {
		return "", fmt.Errorf("agentic: cannot serialize the fact base: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are composing %s's morning digest for %s. Output markdown only: no preamble, no commentary, no code fence around the page itself.\n\n",
		ownerName(in.Owner), in.Day)

	b.WriteString("THE FACT BASE\n\n")
	b.WriteString("Everything you are allowed to state is below. It is the output of aubade's deterministic toolbox: every entry is already cited, and nothing outside it may appear on the page. You are ranking, connecting and writing — you are not adding facts.\n\n")
	b.WriteString("```json\n")
	b.Write(facts)
	b.WriteString("\n```\n\n")

	fmt.Fprintf(&b, "THE TOOLBOX\n\nYou may run exactly one command, and nothing else, up to %d times:\n\n    %s tool <name> [argument] --data %s --today %s --json\n\n",
		in.MaxCalls, in.ToolBin, in.DataDir, in.Today)
	b.WriteString("  commitments | quiet-threads | conflicts | contradictions | dispatchables |\n")
	b.WriteString("  suppressions | staleness | thread <thread-id> | search <query>\n\n")
	b.WriteString("Use `thread` and `search` to look before you rank — a fourteen-message thread deserves reading before it is called urgent. Any other command is denied by the sandbox, so do not try one.\n\n")

	b.WriteString("CITATIONS\n\nEvery factual line ends with one or more citations, in exactly this form, refs copied verbatim from the fact base:\n\n")
	b.WriteString("    [email:e-0042]   [calendar:evt-17]   [note:notes/kickoff.md]   [task:t-3]\n\n")
	b.WriteString("A ref that is not in the fact base is a fabrication. aubade checks every one of them after you finish, and throws the entire page away if a single one does not resolve.\n\n")

	if custom := strings.TrimSpace(in.Customize); custom != "" {
		fmt.Fprintf(&b, "%s\n\n%s\n\n", customizeHeading, custom)
		b.WriteString("Follow that format. It governs the shape of the page only — which sections exist, what order, how long. It does not change what is true: the fact base and the citation rule above still hold exactly as written.\n\n")
	} else {
		b.WriteString(defaultFormat + "\n\n")
	}

	b.WriteString("DO NOT WRITE\n\nNo staleness or missing-source banner, no contradictions section, no \"I'm not sure\" section. aubade appends those itself, verbatim from the fact base, after you are done — they are the honesty floor and they are not yours to shape. Do not summarise them either.\n\n")

	if lines := decisionLines(in.Decisions); len(lines) > 0 {
		b.WriteString("DECISIONS ALREADY TAKEN\n\nThese were settled by a vote across every model runner on this machine, before you were called. Honour them:\n\n")
		for _, line := range lines {
			b.WriteString("  - " + line + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("VOICE\n\nDrafted replies use aubade's base voice, overridden by the user's own tone rules wherever they speak.\n\n")
	b.WriteString("Base voice (" + styles.DefaultVoicePath + "):\n\n")
	b.WriteString(styles.DefaultVoice)
	b.WriteString("\n\n")
	if rules := toneRules(in.Profile); rules != "" {
		b.WriteString("The user's own tone rules, which override every line above wherever the two disagree:\n\n" + rules + "\n\n")
	}
	if people := priorityPeople(in.Profile); people != "" {
		b.WriteString("Who matters, from the user's profile:\n\n" + people + "\n\n")
	}

	b.WriteString("Write the page now.")
	return b.String(), nil
}

// decisionLines renders the consensus outcomes the orchestrator must honour.
func decisionLines(ds []Decision) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if line := strings.TrimSpace(d.Instruction); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// toneRules renders the profile's tone section as the prompt's override layer,
// each rule cited to the line it came from so the model is quoting the user
// rather than being told about them.
func toneRules(p *model.Profile) string {
	if p == nil || len(p.ToneRules) == 0 {
		return ""
	}
	lines := make([]string, 0, len(p.ToneRules))
	for _, r := range p.ToneRules {
		lines = append(lines, fmt.Sprintf("  - %s (%s:%d)", r.Text, p.Path, r.Line))
	}
	return strings.Join(lines, "\n")
}

// priorityPeople renders the profile's priority table. The conditional entries
// keep their sentence: "P0 during the raise, P2 otherwise" is a condition the
// toolbox cannot compute and the orchestrator can read.
func priorityPeople(p *model.Profile) string {
	if p == nil || len(p.People) == 0 {
		return ""
	}
	lines := make([]string, 0, len(p.People))
	for _, person := range p.People {
		line := fmt.Sprintf("  - %s (%s)", person.Name, person.Priority)
		if role := strings.TrimSpace(person.Role); role != "" {
			line += " — " + role
		}
		if note := strings.TrimSpace(person.Note); person.Conditional && note != "" {
			line += " — " + note
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ownerName is whose morning this is, falling back to a description rather than
// to an empty string in a sentence.
func ownerName(p model.Person) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	if email := strings.TrimSpace(p.Email); email != "" {
		return email
	}
	return "the user"
}
