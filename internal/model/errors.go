package model

import (
	"errors"
	"fmt"
	"strings"
)

// ErrSourceMissing is the sentinel behind every "that source was not there"
// error. Providers wrap it in a *MissingSourceError; callers that only need the
// yes/no answer use errors.Is.
var ErrSourceMissing = errors.New("source not found")

// MissingSourceError says a source does not exist. It is recoverable by
// contract: LoadCorpus records it and keeps going, and the digest opens with an
// explicit banner instead of silently thinning (HLD §7).
type MissingSourceError struct {
	Source string // "email" | "calendar" | "note" | "task" | "profile"
	Path   string
}

func (e *MissingSourceError) Error() string {
	return fmt.Sprintf("%s source not found: %s", e.Source, e.Path)
}

// Unwrap makes errors.Is(err, ErrSourceMissing) the one check a caller needs.
func (e *MissingSourceError) Unwrap() error { return ErrSourceMissing }

// ValidationError says a source exists but does not honour its contract.
//
// It is fatal by contract. The alternative — skip the line, carry on — is how a
// digest ends up confidently omitting the one email that mattered, and no
// banner ever appears because nothing noticed. Line is 1-based, or 0 for
// formats and failures that are not line-addressable.
type ValidationError struct {
	Source string // "email" | "calendar" | "note" | "task" | "profile"
	Path   string
	Line   int
	Msg    string
	Err    error // optional underlying cause
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	if e.Path != "" {
		b.WriteString(e.Path)
		if e.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Line)
		}
		b.WriteString(": ")
	}
	if e.Source != "" {
		b.WriteString(e.Source)
		b.WriteString(": ")
	}
	if e.Msg != "" {
		b.WriteString(e.Msg)
	}
	if e.Err != nil {
		if e.Msg != "" {
			b.WriteString(": ")
		}
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *ValidationError) Unwrap() error { return e.Err }
