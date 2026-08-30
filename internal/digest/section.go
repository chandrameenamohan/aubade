package digest

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Sectioning is the page's shape: which of the sample digest's sections a fact
// belongs under, what opens the page, and what is allowed to be left out.
//
// Three decisions are load-bearing:
//
//   - **The section contract is fixed.** The five content sections always
//     render, even when empty, because "nothing needs you today" is an answer
//     and a missing heading is not — a reader must never have to wonder whether
//     a section was quiet or simply lost. Only "I'm not sure" is allowed to
//     disappear, since an empty uncertainty list is not a finding.
//   - **The honesty layer routes first.** Anything the toolbox hinted at the
//     honesty section stays there whatever its priority, and anything marked
//     `unsure` goes to "I'm not sure" whatever its hint. The renderer is not
//     allowed to promote an uncertain fact into an assertion by putting it
//     under a confident heading.
//   - **The page has a bottom.** Each list section takes its highest-ranked
//     items and says out loud how many it held back. A one-pager that quietly
//     grows to three pages has stopped being the product.

// Section caps. Each is the number of lines that section can carry before the
// page stops being a page; the count of what was dropped is always shown.
const (
	capUrgent    = 6
	capDecisions = 5
	capPulse     = 5
	capConflicts = 4
	capNotSure   = 4
	capHonesty   = 8
)

// composer holds the state one page is built from.
type composer struct {
	in    Input
	idx   *index
	loc   *time.Location
	now   time.Time
	day   time.Time
	voice *Voice
}

// buckets is the routed signal set, one ordered slice per section. A struct
// rather than a map: the sections are a fixed, ordered vocabulary, and a map
// here would put the page's shape at the mercy of range order.
type buckets struct {
	oneThing  []ranked
	urgent    []ranked
	decisions []ranked
	pulse     []ranked
	calendar  []ranked
	notSure   []ranked
	honesty   []ranked
	banner    []ranked
}

// compose fills the digest.
func (c *composer) compose(d *Digest) {
	all := rank(c.in.Signals, c.idx, c.now, c.day, c.loc)
	b := c.route(all)

	pick, tied := c.pickOneThing(&b)

	d.Banner = c.items(b.banner, len(b.banner))

	notSure := c.notSureSection(b.notSure)
	if it := c.tieItem(tied); it != nil {
		notSure.Items = append([]Item{*it}, notSure.Items...)
	}

	d.Sections = []Section{
		c.oneThingSection(pick),
		c.listSection(model.SectionUrgentToday, "Urgent To-Do Today",
			"Nothing needs an answer before midnight Pacific.", b.urgent, capUrgent),
		c.listSection(model.SectionDecisions, "Decisions & Approvals Needed",
			"Nobody is blocked on a decision from you.", b.decisions, capDecisions),
		c.listSection(model.SectionPulse, "Team & Product Pulse",
			"No team or product signal worth a line this morning.", b.pulse, capPulse),
		c.calendarSection(b.calendar),
		notSure,
		c.honestySection(b.honesty, len(d.Banner)),
	}

	d.Stats = c.stats(all, d)
}

// route sends every ranked signal to exactly one bucket.
func (c *composer) route(all []ranked) buckets {
	var b buckets
	for _, r := range all {
		switch section(r.Signal) {
		case model.SectionHonesty:
			if c.isBanner(r.Signal) {
				b.banner = append(b.banner, r)
			} else {
				b.honesty = append(b.honesty, r)
			}
		case model.SectionNotSure:
			b.notSure = append(b.notSure, r)
		case model.SectionOneThingNow:
			b.oneThing = append(b.oneThing, r)
		case model.SectionUrgentToday:
			b.urgent = append(b.urgent, r)
		case model.SectionDecisions:
			b.decisions = append(b.decisions, r)
		case model.SectionCalendar:
			b.calendar = append(b.calendar, r)
		default:
			b.pulse = append(b.pulse, r)
		}
	}
	return b
}

// section decides where one signal belongs.
//
// Honesty wins over everything, because a caveat filed under a confident
// heading stops being a caveat. Uncertainty wins over the extractor's hint for
// the same reason: the profile asks to be told "I'm not sure" and shown the
// thread, not to be given a ranked guess.
func section(s model.Signal) string {
	if s.SectionHint == model.SectionHonesty {
		return model.SectionHonesty
	}
	if s.Confidence == model.Unsure {
		return model.SectionNotSure
	}
	if model.IsKnownSectionHint(s.SectionHint) {
		return s.SectionHint
	}
	return model.SectionPulse
}

// isBanner reports whether an honesty signal belongs at the very top of the
// page rather than in the section at the bottom.
//
// The test is what the reader needs *before* trusting anything above it: a
// source that was not there at all, or an inbox past the profile's 24-hour
// freshness budget. Both arrive as certain, high-priority staleness signals. An
// undated note (P3) or a calendar we are merely suspicious of (unsure) is a
// caveat, and a caveat that opens the page trains the reader to skip banners.
func (c *composer) isBanner(s model.Signal) bool {
	return s.Kind == model.KindStaleness &&
		s.Confidence == model.Certain &&
		s.Priority.Rank() <= model.P1.Rank()
}

// pickOneThing chooses the single item that opens the page, and reports any
// genuine tie for it.
//
// Candidates are whatever the toolbox hinted at the one-thing section; if
// nothing did, the best certain item from urgent or decisions stands in, because
// a morning always has a most-important thing even when no extractor was
// confident enough to say so.
//
// A tie is not broken quietly. When two candidates score identically the page
// still opens with one — the order has to be total — but the contest is
// reported under "I'm not sure" with both threads, which is the honest form of
// "I could not tell these apart". That is the deterministic analogue of the
// agentic mode's runner disagreement (SPEC §5).
func (c *composer) pickOneThing(b *buckets) (*ranked, []ranked) {
	pool := b.oneThing
	fallback := false
	if len(pool) == 0 {
		pool = c.fallbackCandidates(b)
		fallback = true
	}
	if len(pool) == 0 {
		return nil, nil
	}

	pick := pool[0]
	var tied []ranked
	for _, other := range pool[1:] {
		if other.Score.Total == pick.Score.Total {
			tied = append(tied, other)
		}
	}

	// Everything not picked stays on the page, one section down.
	if fallback {
		b.urgent = removeSignal(b.urgent, pick.Signal.ID)
		b.decisions = removeSignal(b.decisions, pick.Signal.ID)
	} else {
		b.urgent = append(slices.Clone(b.oneThing[1:]), b.urgent...)
	}
	b.oneThing = nil

	if len(tied) > 0 {
		tied = append([]ranked{pick}, tied...)
	}
	return &pick, tied
}

// fallbackCandidates is the pool the page opens from when no extractor claimed
// the top slot: the certain items from urgent and decisions, in rank order.
func (c *composer) fallbackCandidates(b *buckets) []ranked {
	pool := append(slices.Clone(b.urgent), b.decisions...)
	slices.SortStableFunc(pool, func(x, y ranked) int {
		if x.Score.Total != y.Score.Total {
			return y.Score.Total - x.Score.Total
		}
		return strings.Compare(x.Signal.ID, y.Signal.ID)
	})
	return pool
}

// removeSignal drops the signal with the given id.
func removeSignal(rs []ranked, id string) []ranked {
	out := make([]ranked, 0, len(rs))
	for _, r := range rs {
		if r.Signal.ID != id {
			out = append(out, r)
		}
	}
	return out
}

// oneThingSection renders the page's opening.
func (c *composer) oneThingSection(pick *ranked) Section {
	s := Section{
		ID:        model.SectionOneThingNow,
		Heading:   "If there is one thing you must do right now:",
		Empty:     "Nothing in these sources outranks everything else this morning. That is a real answer, not an empty section — the ranked items below are what there is.",
		Paragraph: true,
	}
	if pick != nil {
		s.Items = []Item{c.item(*pick)}
	}
	return s
}

// listSection renders one capped list section.
func (c *composer) listSection(id, heading, empty string, rs []ranked, limit int) Section {
	s := Section{ID: id, Heading: heading, Empty: empty}
	shown := rs
	if len(shown) > limit {
		shown, s.Overflow = rs[:limit], len(rs)-limit
	}
	s.Items = c.items(shown, len(shown))
	return s
}

// calendarSection is the day itself, then the collisions on it.
//
// The agenda is read straight off the corpus rather than out of a signal: a
// meeting at 10:30 is a fact no extractor needs an opinion about, and the
// sample digest's calendar block is exactly that list. Conflicts are signals,
// and they follow it.
func (c *composer) calendarSection(rs []ranked) Section {
	s := Section{
		ID:      model.SectionCalendar,
		Heading: "Calendar & Personal",
		Empty:   "Nothing on the calendar today, and no collisions to flag.",
	}
	s.Items = c.agenda()

	conflicts := rs
	if len(conflicts) > capConflicts {
		conflicts, s.Overflow = rs[:capConflicts], len(rs)-capConflicts
	}
	s.Items = append(s.Items, c.items(conflicts, len(conflicts))...)
	return s
}

// agenda lists today's live events in clock order.
func (c *composer) agenda() []Item {
	events := make([]*model.CalEvent, 0, len(c.in.Corpus.Events))
	for i := range c.in.Corpus.Events {
		ev := &c.in.Corpus.Events[i]
		if ev.Status == model.StatusCancelled || ev.DeclinedBy(c.in.Owner.Email) {
			continue
		}
		if !sameDay(ev.Start, c.day, c.loc) {
			continue
		}
		events = append(events, ev)
	}
	slices.SortStableFunc(events, func(a, b *model.CalEvent) int {
		if a.AllDay != b.AllDay {
			if a.AllDay {
				return -1
			}
			return 1
		}
		if !a.Start.Equal(b.Start) {
			return a.Start.Compare(b.Start)
		}
		return strings.Compare(a.UID, b.UID)
	})

	out := make([]Item, 0, len(events))
	for _, ev := range events {
		lead := ev.Start.In(c.loc).Format("15:04")
		if ev.AllDay {
			lead = "all day"
		}
		cites := []model.Citation{{Source: model.SourceCalendar, Ref: ev.UID}}
		out = append(out, Item{
			Lead:      lead,
			Body:      eventLine(ev),
			Citations: cites,
			Refs:      c.idx.labels(cites),
		})
	}
	return out
}

// eventLine is the one-line description of an event: what it is, how long, and
// where, when "where" is a room rather than a link.
//
// It opens with the em dash the sample digest puts between a clock time and its
// meeting ("**09:00** — 1:1 with Jordan (30m)"), because on an agenda the lead
// is a timestamp rather than a sentence and the two need visibly separating.
func eventLine(ev *model.CalEvent) string {
	line := strings.TrimSpace(ev.Summary)
	if line == "" {
		line = "(untitled)"
	}
	if !ev.AllDay {
		line += " (" + durationPhrase(ev.Duration()) + ")"
	}
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		line += " — " + loc
	}
	if ev.Status == model.StatusTentative {
		line += " — tentative"
	}
	return "— " + line
}

// durationPhrase renders a meeting length the way a calendar does.
func durationPhrase(d time.Duration) string {
	minutes := int(d.Minutes())
	if minutes < 0 {
		minutes = 0
	}
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%02dm", h, m)
	}
}

// tieItem reports a dead heat for the top of the page as one line rather than
// as duplicates of the tied items, which are already on the page one section
// down. It says which one the page opened with and why — a name, not a
// judgment — so the reader knows the choice was arbitrary and can make it
// themselves.
func (c *composer) tieItem(tied []ranked) *Item {
	if len(tied) < 2 {
		return nil
	}
	titles := make([]string, 0, len(tied))
	var cites []model.Citation
	for _, r := range tied {
		titles = append(titles, quoted(clip(r.Signal.Title, 60)))
		for _, cite := range r.Signal.Citations {
			if !slices.Contains(cites, cite) {
				cites = append(cites, cite)
			}
		}
	}
	return &Item{
		Lead: fmt.Sprintf("%s are tied for the top of your morning.", plural(len(tied), "item", "items")),
		Body: fmt.Sprintf("%s score identically (%d — %s). The page opens with the first of them by name, not by judgment — I could not tell them apart, and both are shown below.",
			strings.Join(titles, " and "), tied[0].Score.Total, tied[0].Score.Why),
		Citations: cites,
		Refs:      c.idx.labels(cites),
	}
}

// notSureSection is the honesty layer's admission that something could not be
// ranked. It is the one section allowed to vanish: an empty uncertainty list is
// not a finding.
func (c *composer) notSureSection(rs []ranked) Section {
	s := Section{ID: model.SectionNotSure, Heading: "I'm not sure"}
	shown := rs
	if len(shown) > capNotSure {
		shown, s.Overflow = rs[:capNotSure], len(rs)-capNotSure
	}
	s.Items = c.items(shown, len(shown))
	return s
}

// honestySection is what the page cannot vouch for: disagreements, caveats, and
// everything the profile told us to hold back.
//
// banners is how many caveats already opened the page. It changes only the
// empty sentence, and it has to: "sources are complete and fresh" printed
// underneath a banner saying the calendar is missing is the page contradicting
// itself, which is a worse failure than the missing calendar.
func (c *composer) honestySection(rs []ranked, banners int) Section {
	empty := "Sources are complete and fresh, nothing was held back, and no two of them disagree."
	if banners > 0 {
		empty = fmt.Sprintf("Nothing further to flag down here — the %s for this page %s at the top of it.",
			plural(banners, "caveat", "caveats"), isAre(banners))
	}
	s := Section{
		ID:      model.SectionHonesty,
		Heading: "Honesty",
		Empty:   empty,
	}

	var (
		contradictions []ranked
		caveats        []ranked
		suppressed     []ranked
	)
	for _, r := range rs {
		switch r.Signal.Kind {
		case model.KindContradictions:
			contradictions = append(contradictions, r)
		case model.KindSuppressions:
			suppressed = append(suppressed, r)
		default:
			caveats = append(caveats, r)
		}
	}

	items := c.items(contradictions, len(contradictions))
	items = append(items, c.items(caveats, len(caveats))...)
	items = append(items, c.suppressionSummary(suppressed)...)
	items = append(items, c.voiceCaveats()...)

	if len(items) > capHonesty {
		s.Overflow = len(items) - capHonesty
		items = items[:capHonesty]
	}
	s.Items = items
	return s
}

// suppressionSummary collapses the held-back items into one line per rule.
//
// Listing thirty suppressed newsletters individually would be honest and
// useless — and would hand the noise the profile just banned a second route
// onto the page. One line per rule, with the count and a sample of the refs,
// says the same thing at the length it is worth.
func (c *composer) suppressionSummary(rs []ranked) []Item {
	type group struct {
		detail string
		count  int
		cites  []model.Citation
	}
	var groups []*group
	byDetail := map[string]*group{}

	for _, r := range rs {
		g := byDetail[r.Signal.Detail]
		if g == nil {
			g = &group{detail: r.Signal.Detail}
			byDetail[r.Signal.Detail] = g
			groups = append(groups, g)
		}
		g.count++
		if len(g.cites) < maxSuppressionCites {
			g.cites = append(g.cites, r.Signal.Citations...)
		}
	}

	out := make([]Item, 0, len(groups))
	for _, g := range groups {
		cites := g.cites
		if len(cites) > maxSuppressionCites {
			cites = cites[:maxSuppressionCites]
		}
		out = append(out, Item{
			Lead:      fmt.Sprintf("Held back %s.", plural(g.count, "item", "items")),
			Body:      capitalize(g.detail),
			Citations: cites,
			Refs:      c.idx.labels(cites),
		})
	}
	return out
}

// maxSuppressionCites is how many receipts a held-back line shows before the
// count carries the rest.
const maxSuppressionCites = 3

// voiceCaveats surfaces the user's own tone rules that the drafter could not
// apply. A tone rule we silently ignored is a promise we silently broke, so it
// is stated in the same place as everything else we cannot vouch for.
func (c *composer) voiceCaveats() []Item {
	if len(c.voice.Unhandled) == 0 {
		return nil
	}
	texts := make([]string, 0, len(c.voice.Unhandled))
	for _, r := range c.voice.Unhandled {
		texts = append(texts, fmt.Sprintf("%q (%s)", r.Text, ruleRef(c.voice.ProfilePath, r.Line)))
	}
	return []Item{{
		Lead: fmt.Sprintf("%s in your tone section were not applied to the drafts below.",
			capitalize(plural(len(texts), "rule", "rules"))),
		Body: "aubade only applies tone rules it can act on mechanically: " + strings.Join(texts, "; ") + ". Everything else in that section is left to you.",
	}}
}

// items turns ranked signals into rendered items, attaching whatever hangs off
// each one.
func (c *composer) items(rs []ranked, n int) []Item {
	out := make([]Item, 0, n)
	for i := 0; i < n && i < len(rs); i++ {
		out = append(out, c.item(rs[i]))
	}
	return out
}

// item renders one signal as a line.
func (c *composer) item(r ranked) Item {
	s := r.Signal
	it := Item{
		SignalID:  s.ID,
		Lead:      sentenceEnd(capitalize(strings.TrimSpace(s.Title))),
		Body:      capitalize(strings.TrimSpace(s.Detail)),
		Citations: s.Citations,
		Refs:      c.idx.labels(s.Citations),
	}
	if s.Kind == model.KindContradictions {
		it.Sides = c.sides(s)
	}
	if s.Kind == model.KindDispatchables {
		it.Draft = c.draftFor(s)
	}
	return it
}

// sides renders a disagreement as its two halves, each with its own citation.
//
// The profile is explicit: "If two sources disagree, tell me — don't pick one
// and hide it." So the signal's own summary stays as the body, and each cited
// source gets its own line saying what *it* claims. Nothing here resolves
// anything.
func (c *composer) sides(s model.Signal) []Side {
	out := make([]Side, 0, len(s.Citations))
	for _, cite := range s.Citations {
		out = append(out, Side{Text: c.idx.describe(cite), Citation: cite, Ref: c.idx.label(cite)})
	}
	return out
}

// stats is what the footer reports.
func (c *composer) stats(all []ranked, d *Digest) Stats {
	st := Stats{Signals: len(all)}
	for _, r := range all {
		if r.Signal.Kind == model.KindSuppressions {
			st.Suppressed++
		}
	}
	for _, s := range d.Sections {
		st.Rendered += len(s.Items)
		for _, it := range s.Items {
			if it.Draft != nil && !it.Draft.Skipped {
				st.Drafts++
			}
		}
	}
	st.Rendered += len(d.Banner)

	corpus := c.in.Corpus
	if len(corpus.Emails) > 0 {
		st.Sources = append(st.Sources, "inbox")
	}
	if len(corpus.Events) > 0 {
		st.Sources = append(st.Sources, "calendar")
	}
	if len(corpus.Notes) > 0 {
		st.Sources = append(st.Sources, "notes")
	}
	if len(corpus.Tasks) > 0 {
		st.Sources = append(st.Sources, "tasks")
	}
	if corpus.Profile != nil {
		st.Sources = append(st.Sources, "profile")
	}
	for _, m := range corpus.Missing {
		st.Missing = append(st.Missing, m.Source)
	}
	return st
}

// plural renders "1 item" / "3 items".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// isAre agrees the verb with a count.
func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
