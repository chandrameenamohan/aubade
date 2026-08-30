package localfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// readFixture loads a testdata file, failing the test rather than the parser if
// the fixture itself is missing.
func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("fixture %s: %v", rel, err)
	}
	return data
}

// wantValidationError asserts the error is a *model.ValidationError on the
// expected line, mentioning the expected text. Line numbers are the point of
// these errors: "the corpus is broken" is not actionable, "inbox.jsonl:2 is"
// is.
func wantValidationError(t *testing.T, err error, wantLine int, wantSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a validation error, got nil")
	}
	var ve *model.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *model.ValidationError, got %T: %v", err, err)
	}
	if ve.Line != wantLine {
		t.Errorf("error on line %d, want line %d (%v)", ve.Line, wantLine, err)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error %q does not mention %q", err.Error(), wantSubstr)
	}
}

func TestParseInbox(t *testing.T) {
	emails, err := parseInbox("inbox.jsonl", readFixture(t, "corpus/inbox.jsonl"))
	if err != nil {
		t.Fatalf("parseInbox: %v", err)
	}
	if len(emails) != 4 {
		t.Fatalf("parsed %d emails, want 4", len(emails))
	}

	first := emails[0]
	if first.ID != "e-001" || first.ThreadID != "t-capt" {
		t.Errorf("first email = %s/%s, want e-001/t-capt", first.ID, first.ThreadID)
	}
	if got := first.TS.Format("2006-01-02T15:04:05Z07:00"); got != "2026-08-27T16:42:00-07:00" {
		t.Errorf("ts = %s, want the RFC3339 value from the file with its zone", got)
	}
	if first.From.Email != "marcus@inflectionpoint.vc" || first.From.Name != "Marcus Webb" {
		t.Errorf("from = %+v, want Marcus Webb <marcus@inflectionpoint.vc>", first.From)
	}
	if len(first.Labels) != 2 || first.Labels[0] != "investor" {
		t.Errorf("labels = %v, want [investor raise]", first.Labels)
	}

	reply := emails[1]
	if reply.InReplyTo != "e-001" {
		t.Errorf("in_reply_to = %q, want e-001", reply.InReplyTo)
	}
	if len(reply.CC) != 1 || reply.CC[0].Email != "ben@wsgr.com" {
		t.Errorf("cc = %+v, want ben@wsgr.com", reply.CC)
	}

	// Order is the file's order: the toolbox and the eval both depend on it.
	for i, want := range []string{"e-001", "e-002", "e-003", "e-004"} {
		if emails[i].ID != want {
			t.Fatalf("emails[%d] = %s, want %s (file order must be preserved)", i, emails[i].ID, want)
		}
	}
}

// Every malformed inbox is fatal and says where. Dropping the bad line and
// carrying on is the failure this whole loader exists to refuse.
func TestParseInboxRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		line   int
		substr string
	}{
		{"truncated json", "malformed/inbox-truncated-json.jsonl", 2, "malformed JSON"},
		{"non-RFC3339 ts", "malformed/inbox-bad-ts.jsonl", 2, "ts"},
		{"duplicate id", "malformed/inbox-duplicate-id.jsonl", 2, "duplicate email id"},
		{"no recipient", "malformed/inbox-no-recipient.jsonl", 1, `"to" is empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseInbox("inbox.jsonl", readFixture(t, tc.file))
			wantValidationError(t, err, tc.line, tc.substr)
		})
	}
}

// Blank lines carry no record, so they cost nothing to tolerate — and a corpus
// written with a trailing newline is the normal case, not an error.
func TestParseInboxToleratesBlankLines(t *testing.T) {
	data := []byte("\n" + string(readFixture(t, "corpus/inbox.jsonl")) + "\n\n")
	emails, err := parseInbox("inbox.jsonl", data)
	if err != nil {
		t.Fatalf("parseInbox: %v", err)
	}
	if len(emails) != 4 {
		t.Fatalf("parsed %d emails, want 4", len(emails))
	}
}

// Two JSON objects on one line is a file that is not JSONL. Silently taking the
// first would lose the second.
func TestParseInboxRejectsTwoValuesOnOneLine(t *testing.T) {
	one := strings.SplitN(string(readFixture(t, "corpus/inbox.jsonl")), "\n", 2)[0]
	_, err := parseInbox("inbox.jsonl", []byte(one+" "+one+"\n"))
	wantValidationError(t, err, 1, "more than one JSON value")
}
