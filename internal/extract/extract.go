// Package extract is aubade's deterministic toolbox: the pure, cited functions
// that turn a normalized Corpus into Signals.
//
// This is the load-bearing half of the keystone decision (HLD §2). Orchestration
// is agentic and may vary run to run; extraction is not. Every fact the digest
// can ever state is produced here, carries at least one citation, and is a
// function of exactly two inputs — the corpus and the anchor day. Same data plus
// same --today gives byte-identical output, which is the property the whole trap
// eval rests on.
//
// Three rules the extractors are written to:
//
//   - A signal with no citation is a claim with no receipt. model.Signal.Validate
//     enforces it; Toolbox.All re-validates before anything is written, so an
//     extractor that forgets is a test failure, not a quietly unfounded line in
//     someone's morning.
//   - Nothing here reads the clock, the filesystem, or a map in iteration order.
//     Ordering is explicit at every step and the final sort is total (it ends on
//     the unique signal id), so there is no run-to-run wobble to chase.
//   - The profile is the policy. Priorities, suppressions, the personal-vs-work
//     distinction and the quiet-thread thresholds all come from profile.md, and
//     the bullet that drove a decision is quoted with its line number in the
//     signal's detail. A rule the parser cannot classify stays inert and visible
//     (Toolbox.UnhandledSuppressions) rather than being silently approximated.
//
// `thread` and `search` are the other two tools. They emit no signals — they are
// read-only investigation surfaces so the orchestrator can look before it ranks
// (HLD §3, the "14-message thread" case).
package extract

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// dayStartHour is when Avery's day begins ("Pacific time. My day starts at
// 6am." — profile.md). It is the instant the toolbox reasons from: ages,
// staleness and overdue-ness are all measured against the anchor day at 06:00
// local, never against the wall clock, because the wall clock would make the
// same corpus grade differently every minute.
const dayStartHour = 6

// Toolbox is the extractor set bound to one corpus and one anchor day.
//
// It is immutable after New: everything derived from the corpus — the thread
// index, the priority map, the suppression set — is computed once, in a fixed
// order, so no extractor can observe a different world than its neighbour.
type Toolbox struct {
	corpus *model.Corpus
	loc    *time.Location

	// day is midnight of the anchor day, and now is that day at 06:00 — the
	// instant "this morning" means.
	day time.Time
	now time.Time

	owner     model.Person
	ownerAddr string

	prio *priorityMap

	emails     map[string]*model.Email
	threads    []*Thread
	threadByID map[string]*Thread

	supp *suppressor
}

// New binds the toolbox to a corpus and an anchor day.
//
// today may be any instant on the anchor day, in any zone; it is reduced to a
// calendar day in loc. A nil loc means the corpus zone (America/Los_Angeles),
// because a date written in this corpus means a Pacific date.
func New(c *model.Corpus, today time.Time, loc *time.Location) (*Toolbox, error) {
	if c == nil {
		return nil, fmt.Errorf("extract: New called with a nil corpus")
	}
	if loc == nil {
		loc = model.Location()
	}
	if today.IsZero() {
		return nil, fmt.Errorf("extract: New called with a zero --today; the anchor day is required for determinism")
	}

	local := today.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	t := &Toolbox{
		corpus: c,
		loc:    loc,
		day:    day,
		now:    day.Add(dayStartHour * time.Hour),
	}
	t.owner = resolveOwner(c)
	t.ownerAddr = normAddr(t.owner.Email)
	t.indexEmails()
	t.prio = newPriorityMap(c.Profile)
	t.supp = newSuppressor(t)
	return t, nil
}

// ParseToday reads a --today value ("2026-08-30") in loc. An empty value is an
// error rather than a silent fallback to the system clock: callers that want
// the clock must say so, because "today" is the one input that decides whether
// a deadline is overdue.
func ParseToday(v string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = model.Location()
	}
	s := strings.TrimSpace(v)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty --today; want YYYY-MM-DD")
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid --today %q; want YYYY-MM-DD", v)
}

// Today is the anchor day (midnight, in the corpus zone).
func (t *Toolbox) Today() time.Time { return t.day }

// Now is the anchor instant: the anchor day at 06:00 local, the moment the
// digest is written for.
func (t *Toolbox) Now() time.Time { return t.now }

// Owner is whose digest this is.
func (t *Toolbox) Owner() model.Person { return t.owner }

// Location is the zone every date in the output is expressed in.
func (t *Toolbox) Location() *time.Location { return t.loc }

// resolveOwner decides whose inbox this is.
//
// profile.md's "# " title and the address in it are authoritative. Without a
// profile the corpus still has to be usable, so we fall back to the address
// that appears most often across senders and recipients — with a lexical
// tie-break, because a map range would make the fallback non-deterministic and
// determinism is the one thing this package cannot trade away.
func resolveOwner(c *model.Corpus) model.Person {
	if c.Profile != nil && strings.TrimSpace(c.Profile.Owner.Email) != "" {
		return c.Profile.Owner
	}

	count := map[string]int{}
	name := map[string]string{}
	note := func(p model.Person) {
		a := normAddr(p.Email)
		if a == "" {
			return
		}
		count[a]++
		if name[a] == "" {
			name[a] = strings.TrimSpace(p.Name)
		}
	}
	for _, e := range c.Emails {
		note(e.From)
		for _, p := range e.To {
			note(p)
		}
	}

	best, bestN := "", 0
	for addr, n := range count {
		if n > bestN || (n == bestN && addr < best) {
			best, bestN = addr, n
		}
	}
	if best == "" {
		if c.Profile != nil {
			return c.Profile.Owner
		}
		return model.Person{}
	}
	return model.Person{Name: name[best], Email: best}
}

// indexEmails builds the by-id and by-thread indexes.
//
// Messages inside a thread are ordered by timestamp and then by id, so two
// messages that share a second still land in one fixed order.
func (t *Toolbox) indexEmails() {
	t.emails = make(map[string]*model.Email, len(t.corpus.Emails))
	t.threadByID = make(map[string]*Thread)

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		t.emails[e.ID] = e

		th := t.threadByID[e.ThreadID]
		if th == nil {
			th = &Thread{ID: e.ThreadID}
			t.threadByID[e.ThreadID] = th
			t.threads = append(t.threads, th)
		}
		th.Messages = append(th.Messages, *e)
	}

	slices.SortFunc(t.threads, func(a, b *Thread) int { return strings.Compare(a.ID, b.ID) })
	for _, th := range t.threads {
		sort.SliceStable(th.Messages, func(i, j int) bool {
			a, b := th.Messages[i], th.Messages[j]
			if !a.TS.Equal(b.TS) {
				return a.TS.Before(b.TS)
			}
			return a.ID < b.ID
		})
		th.finish(t.ownerAddr)
	}
}

// Threads returns every thread, ordered by thread id.
func (t *Toolbox) Threads() []*Thread { return t.threads }

// Extractor is one signal-producing tool.
type Extractor func() (model.Signals, error)

// namedExtractor pairs an extractor with the kind it produces.
type namedExtractor struct {
	Kind string
	Run  Extractor
}

// extractors is the fixed run order for `aubade signals` and `--no-llm`. It is
// model.KnownKinds' order, which is also traps.json's vocabulary, so a trap can
// name the extractor that missed it without a translation table.
func (t *Toolbox) extractors() []namedExtractor {
	return []namedExtractor{
		{model.KindCommitments, t.Commitments},
		{model.KindQuietThreads, t.QuietThreads},
		{model.KindConflicts, t.Conflicts},
		{model.KindContradictions, t.Contradictions},
		{model.KindDispatchables, t.Dispatchables},
		{model.KindSuppressions, t.Suppressions},
		{model.KindStaleness, t.Staleness},
	}
}

// Kinds lists the signal-emitting extractor names in run order.
func Kinds() []string { return slices.Clone(model.KnownKinds) }

// All runs every extractor in the fixed order and returns the merged, sorted,
// validated signal set — the content of out/signals.json.
//
// Validation happens here rather than at each call site because this is the
// single funnel every signal passes through on its way to disk: an uncitable
// signal must never reach a file the digest is composed from.
func (t *Toolbox) All() (model.Signals, error) {
	var out model.Signals
	for _, x := range t.extractors() {
		ss, err := x.Run()
		if err != nil {
			return nil, fmt.Errorf("extractor %s: %w", x.Kind, err)
		}
		out = append(out, ss...)
	}
	SortSignals(out)
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("extract: produced an invalid signal set: %w", err)
	}
	return out, nil
}

// Result is one tool call's output. Exactly one of its payload fields is set:
// the seven extractors fill Signals, `thread` fills Thread, `search` fills
// Search.
//
// It is a carrier for the caller, not the wire shape — what goes over the wire
// is Payload().
type Result struct {
	Tool    string
	Signals model.Signals
	Thread  *ThreadView
	Search  *SearchResult
}

// Payload is the value to marshal for `--json`: the bare signal array, the
// thread, or the search result. Callers get the data itself rather than an
// envelope, so `aubade tool commitments --json | jq '.[0].citations'` works and
// signals.json and tool output stay one dialect.
func (r *Result) Payload() any {
	switch {
	case r.Thread != nil:
		return r.Thread
	case r.Search != nil:
		return r.Search
	default:
		if r.Signals == nil {
			return model.Signals{}
		}
		return r.Signals
	}
}

// Run dispatches one `aubade tool <name> [arg]` call.
//
// The name set is the published toolbox surface; an unknown name lists the
// alternatives, because the caller is often an agent that guessed.
func (t *Toolbox) Run(name, arg string) (*Result, error) {
	res := &Result{Tool: name}

	switch name {
	case "thread":
		v, err := t.Thread(arg)
		if err != nil {
			return nil, err
		}
		res.Thread = v
		return res, nil
	case "search":
		v, err := t.Search(arg)
		if err != nil {
			return nil, err
		}
		res.Search = v
		return res, nil
	}

	for _, x := range t.extractors() {
		if x.Kind != name {
			continue
		}
		ss, err := x.Run()
		if err != nil {
			return nil, fmt.Errorf("extractor %s: %w", name, err)
		}
		SortSignals(ss)
		if err := ss.Validate(); err != nil {
			return nil, fmt.Errorf("extractor %s produced an invalid signal: %w", name, err)
		}
		res.Signals = ss
		return res, nil
	}

	return nil, fmt.Errorf("unknown tool %q; one of: %s", name, strings.Join(ToolNames(), ", "))
}

// ToolNames is the full toolbox surface, extractors first then the two
// investigation tools.
func ToolNames() []string {
	return append(slices.Clone(model.KnownKinds), "thread", "search")
}
