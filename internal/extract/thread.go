package extract

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Thread is one conversation: its messages in time order, plus the two facts
// every extractor asks about it — who it is waiting on, and since when.
//
// "Waiting on" is the whole reason this type exists. "About to drop" (HLD §11)
// is a statement about what did *not* happen, and the only way to compute it is
// to know whose turn it is and how long they have had it.
type Thread struct {
	ID       string        `json:"thread_id"`
	Subject  string        `json:"subject"`
	Messages []model.Email `json:"messages"`

	// Counterparts are the non-owner participants, deduplicated and ordered by
	// address.
	Counterparts []model.Person `json:"counterparts"`

	// AwaitingOwner is true when the last word was someone else's and it left
	// something open; AwaitingReply is true when the owner spoke last and asked
	// for something. Both false means the thread is finished, which is a
	// suppression candidate, not a signal.
	AwaitingOwner bool `json:"awaiting_owner"`
	AwaitingReply bool `json:"awaiting_reply"`

	// OwnerHadLastWord records who spoke last, independent of whether anything
	// was left open. The profile suppresses long threads where the owner had
	// the last word and nobody replied.
	OwnerHadLastWord bool `json:"owner_had_last_word"`
}

// finish computes the derived fields once the messages are in order.
func (th *Thread) finish(ownerAddr string) {
	if len(th.Messages) == 0 {
		return
	}
	th.Subject = normalizeSubject(th.Messages[0].Subject)

	seen := map[string]bool{}
	for _, m := range th.Messages {
		for _, p := range append([]model.Person{m.From}, append(m.To, m.CC...)...) {
			a := normAddr(p.Email)
			if a == "" || a == ownerAddr || seen[a] {
				continue
			}
			seen[a] = true
			th.Counterparts = append(th.Counterparts, p)
		}
	}
	slices.SortFunc(th.Counterparts, func(a, b model.Person) int {
		return strings.Compare(normAddr(a.Email), normAddr(b.Email))
	})

	last := th.Last()
	th.OwnerHadLastWord = normAddr(last.From.Email) == ownerAddr
	if th.OwnerHadLastWord {
		th.AwaitingReply = asksSomething(last.Body) || asksSomething(last.Subject)
	} else {
		// A counterparty's message leaves something open if it asks for
		// anything, or if it opens the thread — an unanswered first contact is
		// open by construction.
		th.AwaitingOwner = asksSomething(last.Body) || asksSomething(last.Subject) || len(th.Messages) == 1
	}
}

// Last is the most recent message.
func (th *Thread) Last() model.Email { return th.Messages[len(th.Messages)-1] }

// First is the oldest message.
func (th *Thread) First() model.Email { return th.Messages[0] }

// After returns the messages strictly newer than t (ties broken by id, matching
// the thread's own ordering).
func (th *Thread) After(e model.Email) []model.Email {
	for i, m := range th.Messages {
		if m.ID == e.ID {
			return th.Messages[i+1:]
		}
	}
	return nil
}

// ThreadView is what `aubade tool thread <id>` returns: the whole conversation,
// each message carrying its own citation so the orchestrator can quote a line
// and cite it in the same breath.
type ThreadView struct {
	ThreadID     string          `json:"thread_id"`
	Subject      string          `json:"subject"`
	MessageCount int             `json:"message_count"`
	Counterparts []model.Person  `json:"counterparts"`
	WaitingOn    string          `json:"waiting_on"` // "owner" | "counterpart" | "nobody"
	QuietFor     string          `json:"quiet_for"`  // e.g. "3 business days"
	Messages     []ThreadMessage `json:"messages"`
}

// ThreadMessage is one message as the investigation tool renders it.
type ThreadMessage struct {
	ID        string          `json:"id"`
	TS        time.Time       `json:"ts"`
	From      model.Person    `json:"from"`
	To        []model.Person  `json:"to"`
	CC        []model.Person  `json:"cc,omitempty"`
	Subject   string          `json:"subject"`
	Body      string          `json:"body"`
	FromOwner bool            `json:"from_owner"`
	Citation  model.Citation  `json:"citation"`
	Labels    []string        `json:"labels,omitempty"`
	Suppress  *SuppressedNote `json:"suppressed,omitempty"`
}

// SuppressedNote says which profile rule held an item back, quoting the user's
// own words and the line they are on.
type SuppressedNote struct {
	Rule string `json:"rule"`
	Line int    `json:"line"`
	Why  string `json:"why"`
}

// Thread reads one conversation.
//
// The argument is a thread id, but an email id is accepted too and resolves to
// its thread: every citation the toolbox emits carries an email id, so an agent
// holding one should not have to go looking for the thread it belongs to.
func (t *Toolbox) Thread(id string) (*ThreadView, error) {
	q := strings.TrimSpace(id)
	if q == "" {
		return nil, fmt.Errorf("thread requires a thread id (or an email id)")
	}

	th := t.threadByID[q]
	if th == nil {
		if e, ok := t.emails[q]; ok {
			th = t.threadByID[e.ThreadID]
		}
	}
	if th == nil {
		return nil, fmt.Errorf("no thread %q in %s; try `aubade tool search %s`", q, t.corpus.Source, q)
	}

	v := &ThreadView{
		ThreadID:     th.ID,
		Subject:      th.Subject,
		MessageCount: len(th.Messages),
		Counterparts: th.Counterparts,
		WaitingOn:    waitingOn(th),
		QuietFor:     businessDayPhrase(businessDaysBetween(th.Last().TS, t.now, t.loc)),
	}
	for _, m := range th.Messages {
		tm := ThreadMessage{
			ID:        m.ID,
			TS:        m.TS.In(t.loc),
			From:      m.From,
			To:        m.To,
			CC:        m.CC,
			Subject:   m.Subject,
			Body:      m.Body,
			FromOwner: normAddr(m.From.Email) == t.ownerAddr,
			Citation:  model.Citation{Source: model.SourceEmail, Ref: m.ID},
			Labels:    m.Labels,
		}
		if s, ok := t.supp.email(m.ID); ok {
			tm.Suppress = &SuppressedNote{Rule: s.Rule.Text, Line: s.Rule.Line, Why: s.Why}
		}
		v.Messages = append(v.Messages, tm)
	}
	return v, nil
}

func waitingOn(th *Thread) string {
	switch {
	case th.AwaitingOwner:
		return "owner"
	case th.AwaitingReply:
		return "counterpart"
	default:
		return "nobody"
	}
}

// SearchResult is what `aubade tool search <q>` returns.
type SearchResult struct {
	Query string      `json:"query"`
	Total int         `json:"total"`
	Hits  []SearchHit `json:"hits"`
}

// SearchHit is one match, already carrying the citation that would let a digest
// line quote it.
type SearchHit struct {
	Source   model.Source   `json:"source"`
	Ref      string         `json:"ref"`
	Title    string         `json:"title"`
	Snippet  string         `json:"snippet"`
	TS       *time.Time     `json:"ts,omitempty"`
	ThreadID string         `json:"thread_id,omitempty"`
	Score    int            `json:"score"`
	Citation model.Citation `json:"citation"`
}

// searchLimit caps a result set. Search exists so an agent can look before it
// ranks; handing it 400 hits is the same as handing it the corpus.
const searchLimit = 25

// Search runs a token search across all four sources.
//
// Scoring is deliberately crude and explainable: each distinct query token
// scores 3 in a title/subject/sender, 1 in a body, and the full query as a
// phrase scores a further 4. Nobody has to trust a relevance model to read the
// result — the score is reproducible by hand — and 500 emails do not need a
// retrieval engine (HLD §5, RAG rejected).
func (t *Toolbox) Search(query string) (*SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("search requires a query")
	}
	tokens := searchTokens(q)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("search query %q has nothing to match on", query)
	}
	phrase := strings.ToLower(q)

	res := &SearchResult{Query: q}
	add := func(h SearchHit, strong, weak string) {
		score := 0
		s, w := strings.ToLower(strong), strings.ToLower(weak)
		for _, tok := range tokens {
			switch {
			case strings.Contains(s, tok):
				score += 3
			case strings.Contains(w, tok):
				score++
			}
		}
		if score == 0 {
			return
		}
		if strings.Contains(s, phrase) || strings.Contains(w, phrase) {
			score += 4
		}
		h.Score = score
		h.Citation = model.Citation{Source: h.Source, Ref: h.Ref}
		h.Snippet = snippet(weak, tokens)
		if h.Snippet == "" {
			h.Snippet = snippet(strong, tokens)
		}
		res.Hits = append(res.Hits, h)
	}

	for i := range t.corpus.Emails {
		e := &t.corpus.Emails[i]
		ts := e.TS.In(t.loc)
		add(SearchHit{
			Source:   model.SourceEmail,
			Ref:      e.ID,
			Title:    e.Subject,
			TS:       &ts,
			ThreadID: e.ThreadID,
		}, e.Subject+" "+e.From.String(), e.Body)
	}
	for i := range t.corpus.Events {
		ev := &t.corpus.Events[i]
		ts := ev.Start.In(t.loc)
		add(SearchHit{
			Source: model.SourceCalendar,
			Ref:    ev.UID,
			Title:  ev.Summary,
			TS:     &ts,
		}, ev.Summary+" "+ev.Location+" "+ev.Organizer.String(), ev.Description)
	}
	for i := range t.corpus.Notes {
		n := &t.corpus.Notes[i]
		hit := SearchHit{Source: model.SourceNote, Ref: n.Path, Title: n.Title}
		if n.HasDate() {
			d := n.Date.In(t.loc)
			hit.TS = &d
		}
		add(hit, n.Title+" "+n.Path, n.Body)
	}
	for i := range t.corpus.Tasks {
		task := &t.corpus.Tasks[i]
		hit := SearchHit{Source: model.SourceTask, Ref: task.ID, Title: task.Title}
		if task.HasDue() {
			d := task.Due.In(t.loc)
			hit.TS = &d
		}
		// A task is all title: there is no body to snippet, so the owner joins
		// the strong field rather than becoming a one-word excerpt.
		add(hit, task.Title+" "+task.Owner, "")
	}

	// Score first, then newest first, then ref — a total order, so the same
	// query over the same corpus always returns the same page.
	sort.SliceStable(res.Hits, func(i, j int) bool {
		a, b := res.Hits[i], res.Hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		at, bt := timeOrZero(a.TS), timeOrZero(b.TS)
		if !at.Equal(bt) {
			return at.After(bt)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Ref < b.Ref
	})

	res.Total = len(res.Hits)
	if len(res.Hits) > searchLimit {
		res.Hits = res.Hits[:searchLimit]
	}
	return res, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
