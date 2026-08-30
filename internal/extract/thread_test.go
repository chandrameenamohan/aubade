package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// `thread` is the orchestrator's read-before-you-rank tool. It has to answer the
// question the ranking needs — whose turn is it, and for how long — not just
// dump messages.
func TestThreadReadsAConversationInOrder(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	v, err := tb.Thread("t-northstar")
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if v.MessageCount != 3 {
		t.Fatalf("MessageCount = %d, want 3", v.MessageCount)
	}
	var ids []string
	for _, m := range v.Messages {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); got != "e-020,e-021,e-022" {
		t.Errorf("messages out of order: %s", got)
	}
	if v.WaitingOn != "owner" {
		t.Errorf("WaitingOn = %q, want owner", v.WaitingOn)
	}
	if v.QuietFor != "3 business days" {
		t.Errorf("QuietFor = %q, want 3 business days", v.QuietFor)
	}
	if !v.Messages[1].FromOwner {
		t.Error("the middle message is the owner's and should be marked so")
	}
	if v.Messages[0].Citation.Ref != "e-020" || v.Messages[0].Citation.Source != model.SourceEmail {
		t.Errorf("each message must carry its own citation, got %+v", v.Messages[0].Citation)
	}
}

// An agent holding a citation holds an email id, not a thread id. Making it go
// looking for the thread would be a papercut in the one place the tool exists
// to remove one.
func TestThreadResolvesAnEmailID(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	v, err := tb.Thread("e-021")
	if err != nil {
		t.Fatalf("Thread(email id): %v", err)
	}
	if v.ThreadID != "t-northstar" {
		t.Errorf("ThreadID = %q, want t-northstar", v.ThreadID)
	}
}

// A suppressed message is still readable — it is marked, not hidden. An agent
// investigating a thread needs to know the digest dropped something and why.
func TestThreadMarksSuppressedMessages(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	v, err := tb.Thread("t-news")
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(v.Messages) != 1 || v.Messages[0].Suppress == nil {
		t.Fatalf("the newsletter should be readable and marked suppressed: %+v", v.Messages)
	}
	if v.Messages[0].Suppress.Line == 0 {
		t.Error("the suppression note should name the profile line")
	}
}

func TestThreadUnknownIDIsAnError(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	if _, err := tb.Thread("t-nope"); err == nil {
		t.Fatal("expected an error for an unknown thread")
	} else if !strings.Contains(err.Error(), "search") {
		t.Errorf("the error should point at the tool that would find it: %v", err)
	}
	if _, err := tb.Thread("  "); err == nil {
		t.Error("expected an error for an empty thread id")
	}
}

// Search scores are reproducible by hand: 3 per token in a title or sender, 1
// in a body, 4 more for the whole query as a phrase. Nobody has to trust a
// relevance model to read the result.
func TestSearchRanksTitlesAboveBodies(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	res, err := tb.Search("rollout")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatal("no hits for a word that appears in a subject, a body and a task")
	}
	for i := 1; i < len(res.Hits); i++ {
		if res.Hits[i-1].Score < res.Hits[i].Score {
			t.Fatalf("hits are not sorted by score: %d before %d", res.Hits[i-1].Score, res.Hits[i].Score)
		}
	}
	if res.Hits[0].Citation.Ref == "" {
		t.Error("every hit must carry a citation")
	}

	// e-003 has "rollout" in its subject; e-013 only in its body.
	score := map[string]int{}
	for _, h := range res.Hits {
		score[h.Ref] = h.Score
	}
	if score["e-003"] <= score["e-013"] {
		t.Errorf("a subject match (%d) must outrank a body match (%d)", score["e-003"], score["e-013"])
	}
}

// Search reaches all four sources, because an agent asking about a commitment
// may need the note or the task that records it.
func TestSearchCoversEverySource(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	cases := map[string]model.Source{
		"pediatrician": model.SourceCalendar,
		"hiring plan":  model.SourceNote,
		"cost model":   model.SourceTask,
		"diligence":    model.SourceEmail,
	}
	for query, want := range cases {
		res, err := tb.Search(query)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		found := false
		for _, h := range res.Hits {
			if h.Source == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Search(%q) returned no %s hit: %+v", query, want, res.Hits)
		}
	}
}

func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	for _, q := range []string{"", "   ", "-"} {
		if _, err := tb.Search(q); err == nil {
			t.Errorf("Search(%q) should be an error, not an empty page", q)
		}
	}
}
