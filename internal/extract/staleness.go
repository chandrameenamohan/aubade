package extract

import (
	"fmt"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Staleness is the extractor that makes the digest able to say "I might be
// wrong about this". The profile asks for it in as many words — "If the inbox
// data is older than 24 hours, say so" — and SPEC §7 makes fabricated certainty
// an eval failure.
//
// Four things get reported:
//
//   - a source that was not there at all (LoadCorpus records these rather than
//     failing, HLD §7),
//   - an inbox whose newest message predates the freshness budget,
//   - a calendar with nothing on or after the anchor day, which usually means a
//     stale export rather than an empty week,
//   - notes carrying no date of their own, which cannot be aged at all.
//
// Every one of them cites something. A missing source cites its conventional
// corpus-relative name — "calendar.ics" is exactly what a reader goes looking
// for, and unlike the absolute path the provider reports, it means the same
// thing on every machine.

// inboxFreshness is the profile's budget: "If the inbox data is older than 24
// hours, say so."
const inboxFreshness = 24 * time.Hour

// Staleness reports what the digest cannot fully vouch for.
func (t *Toolbox) Staleness() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	out = append(out, t.missingSourceSignals(g)...)
	if s, ok := t.staleInbox(g); ok {
		out = append(out, s)
	}
	if s, ok := t.staleCalendar(g); ok {
		out = append(out, s)
	}
	out = append(out, t.undatedNotes(g)...)
	return out, nil
}

// missingSourceSignals turns each recorded missing source into a signal.
//
// A missing profile is P1 rather than P0: the digest still works, but every
// priority in it is a default rather than the user's own weighting, and that is
// worth saying out loud.
func (t *Toolbox) missingSourceSignals(g *ids) model.Signals {
	var out model.Signals
	for _, m := range t.corpus.Missing {
		source := model.Source(m.Source)
		ref := sourcePaths[m.Source]
		if ref == "" {
			ref = m.Source
		}

		priority := model.P0
		detail := fmt.Sprintf("%s. Everything the %s source would have contributed is absent from this digest, not merely quiet.", m.Reason, m.Source)
		if !source.Valid() {
			// The only source outside the four citable ones is the profile.
			// It is settings rather than data, but it is still a markdown file
			// in the corpus, so it cites as a note by its path — a reader
			// following the ref lands on exactly the file that was not there.
			source = model.SourceNote
			priority = model.P1
			detail = fmt.Sprintf("%s. Priorities and suppressions fall back to aubade defaults.", m.Reason)
		}

		out = append(out, model.Signal{
			ID:          g.next(model.KindStaleness, "missing", m.Source),
			Kind:        model.KindStaleness,
			Priority:    priority,
			Title:       "missing source: " + m.Source,
			Detail:      detail,
			Citations:   []model.Citation{{Source: source, Ref: ref}},
			SectionHint: model.SectionHonesty,
			Confidence:  model.Certain,
		})
	}
	return out
}

// staleInbox reports an inbox whose newest message is older than the profile's
// freshness budget.
func (t *Toolbox) staleInbox(g *ids) (model.Signal, bool) {
	newest, ok := t.newestEmail()
	if !ok {
		return model.Signal{}, false
	}
	age := t.now.Sub(newest.TS)
	if age < inboxFreshness {
		return model.Signal{}, false
	}
	return model.Signal{
		ID:       g.next(model.KindStaleness, "inbox"),
		Kind:     model.KindStaleness,
		Priority: model.P1,
		Title:    fmt.Sprintf("inbox is %s old", roundHours(age)),
		Detail: fmt.Sprintf("Newest message is %s from %s; the budget is %s (%s). Anything sent since is not in this digest.",
			newest.TS.In(t.loc).Format("Mon 2 Jan 15:04"), newest.From.String(),
			roundHours(inboxFreshness), t.honestyRuleRef()),
		Citations:   []model.Citation{emailCite(newest.ID)},
		SectionHint: model.SectionHonesty,
		Confidence:  model.Certain,
	}, true
}

// staleCalendar reports a calendar with nothing at or after the anchor day.
func (t *Toolbox) staleCalendar(g *ids) (model.Signal, bool) {
	if t.corpus.IsMissing("calendar") || len(t.corpus.Events) == 0 {
		return model.Signal{}, false
	}
	var latest *model.CalEvent
	for i := range t.corpus.Events {
		ev := &t.corpus.Events[i]
		if !ev.Start.Before(t.day) {
			return model.Signal{}, false
		}
		if latest == nil || ev.Start.After(latest.Start) {
			latest = ev
		}
	}
	return model.Signal{
		ID:       g.next(model.KindStaleness, "calendar"),
		Kind:     model.KindStaleness,
		Priority: model.P1,
		Title:    "calendar has nothing on or after today",
		Detail: fmt.Sprintf("The last event in the export is %s on %s. An empty day is possible; a stale export is likelier.",
			quote(latest.Summary), latest.Start.In(t.loc).Format("Mon 2 Jan")),
		Citations:   []model.Citation{eventCite(latest.UID)},
		SectionHint: model.SectionHonesty,
		Confidence:  model.Unsure,
	}, true
}

// undatedNotes reports notes that carry no date and therefore cannot be aged.
func (t *Toolbox) undatedNotes(g *ids) model.Signals {
	var out model.Signals
	for i := range t.corpus.Notes {
		n := &t.corpus.Notes[i]
		if n.HasDate() {
			continue
		}
		out = append(out, model.Signal{
			ID:       g.next(model.KindStaleness, "undated", n.Path),
			Kind:     model.KindStaleness,
			Priority: model.P3,
			Title:    "undated note: " + n.Path,
			Detail: fmt.Sprintf("%s carries no date in its front matter, so nothing here can say how old it is. Anything drawn from it is undated too.",
				quote(n.Title)),
			Citations:   []model.Citation{noteCite(n.Path)},
			SectionHint: model.SectionHonesty,
			Confidence:  model.Certain,
		})
	}
	return out
}

// newestEmail is the most recent message at or before the anchor instant.
func (t *Toolbox) newestEmail() (model.Email, bool) {
	var (
		newest model.Email
		found  bool
	)
	for i := range t.corpus.Emails {
		e := t.corpus.Emails[i]
		if e.TS.After(t.now) {
			continue
		}
		if !found || e.TS.After(newest.TS) || (e.TS.Equal(newest.TS) && e.ID > newest.ID) {
			newest, found = e, true
		}
	}
	return newest, found
}

// honestyRuleRef cites the profile bullet that set the freshness budget.
func (t *Toolbox) honestyRuleRef() string {
	p := t.corpus.Profile
	if p == nil {
		return "aubade default"
	}
	for _, r := range p.HonestyRules {
		if containsWord(r.Text, "24 hours") || containsWord(r.Text, "older") {
			return t.ruleRef(r)
		}
	}
	return "aubade default"
}

// roundHours renders a duration at the resolution a staleness banner is honest
// at: whole hours up to two days, whole days past that. "261 hours old" is
// technically true and useless.
func roundHours(d time.Duration) string {
	h := int(d.Round(time.Hour).Hours())
	switch {
	case h == 1:
		return "1 hour"
	case h <= 48:
		return fmt.Sprintf("%d hours", h)
	}
	days := int(d.Round(24*time.Hour).Hours() / 24)
	return fmt.Sprintf("%d days", days)
}
