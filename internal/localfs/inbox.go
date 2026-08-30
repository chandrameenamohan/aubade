package localfs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// maxEmailLine bounds one JSONL record. Email bodies are prose, not payloads;
// a megabyte-long line means the file is not what we think it is, and a scanner
// error saying so beats an unbounded allocation.
const maxEmailLine = 1 << 20

// parseInbox reads inbox.jsonl: one email object per line, in file order.
//
// Blank lines are tolerated (they carry no record and lose nothing). Anything
// else that does not parse — bad JSON, a second value on the line, a missing
// required field, a repeated id — stops the load with the line number attached.
// Unknown fields are kept out of the struct but not rejected, so a corpus
// written by a newer generator still loads here.
func parseInbox(path string, data []byte) ([]model.Email, error) {
	var (
		emails []model.Email
		seen   = map[string]int{}
		sc     = bufio.NewScanner(bytes.NewReader(data))
		lineNo int
	)
	sc.Buffer(make([]byte, 0, 64*1024), maxEmailLine)

	fail := func(line int, msg string, err error) error {
		return &model.ValidationError{
			Source: string(model.SourceEmail),
			Path:   path,
			Line:   line,
			Msg:    msg,
			Err:    err,
		}
	}

	for sc.Scan() {
		lineNo++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}

		var e model.Email
		dec := json.NewDecoder(bytes.NewReader(raw))
		if err := dec.Decode(&e); err != nil {
			// `ts` is the only time-typed field in the contract, and
			// encoding/json drops the field name when time parsing fails —
			// so name it here rather than hand back a bare parse error.
			var pe *time.ParseError
			if errors.As(err, &pe) {
				return nil, fail(lineNo, `field "ts" is not RFC3339 with a zone`, err)
			}
			return nil, fail(lineNo, "malformed JSON", err)
		}
		if dec.More() {
			return nil, fail(lineNo, "more than one JSON value on this line; inbox.jsonl is one email per line", nil)
		}
		if err := e.Validate(); err != nil {
			return nil, fail(lineNo, "", err)
		}
		if first, dup := seen[e.ID]; dup {
			return nil, fail(lineNo, fmt.Sprintf("duplicate email id %q (first seen on line %d)", e.ID, first), nil)
		}
		seen[e.ID] = lineNo
		emails = append(emails, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fail(lineNo+1, "cannot read line", err)
	}
	return emails, nil
}
