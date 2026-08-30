package eval

import (
	"slices"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The half of the grader that reads signals.json.
//
// A trap names the artefacts it planted; a signal names the artefacts it is
// about. Grading is therefore an index lookup rather than a text search, and
// that is the whole reason traps.json carries {source, ref} citations instead of
// bare ids (datagen/trap.go says so): "email:m-capt-02" resolves against exactly
// one source, so a trap can never be graded against the wrong record.

// signalIndex maps each cited artefact to the signals that cite it.
type signalIndex struct {
	byRef map[model.Citation][]model.Signal
}

func newSignalIndex(ss model.Signals) *signalIndex {
	idx := &signalIndex{byRef: make(map[model.Citation][]model.Signal, len(ss))}
	for _, s := range ss {
		for _, c := range s.Citations {
			idx.byRef[c] = append(idx.byRef[c], s)
		}
	}
	return idx
}

// evidence is what the fact base says about one trap's artefacts.
type evidence struct {
	// by is every signal kind that surfaced the trap, in extractor order.
	by []string

	// suppressed reports an audit record: aubade saw the item and held it back.
	suppressed bool

	// rule is the profile rule quoted by that audit record.
	rule string
}

// evidence walks a trap's planted refs and reports what cited them.
func (idx *signalIndex) evidence(refs []model.Citation) evidence {
	var ev evidence
	for _, ref := range refs {
		for _, s := range idx.byRef[ref] {
			if isAuditRecord(s) {
				ev.suppressed = true
				if ev.rule == "" {
					ev.rule = suppressionRule(s)
				}
				continue
			}
			if !slices.Contains(ev.by, s.Kind) {
				ev.by = append(ev.by, s.Kind)
			}
		}
	}
	// Extractor order, so two runs never name the same set in two orders and a
	// scorecard diff means something changed rather than something shuffled.
	slices.SortFunc(ev.by, func(a, b string) int {
		ai, bi := slices.Index(model.KnownKinds, a), slices.Index(model.KnownKinds, b)
		if ai != bi {
			return ai - bi
		}
		return strings.Compare(a, b)
	})
	return ev
}

// isAuditRecord reports whether a signal is aubade's record of holding an item
// back rather than a claim about it. Both halves matter: the kind says it came
// from the suppression pass, and the honesty section says it was filed as an
// admission. The recruiter pattern is a `suppressions` signal in the pulse
// section — a real finding — and it is correctly not an audit record.
func isAuditRecord(s model.Signal) bool {
	return s.Kind == model.KindSuppressions && s.SectionHint == model.SectionHonesty
}

// suppressionRule pulls the user's own bullet out of a suppression record's
// detail line, which renders as: reason — "the rule text" (profile.md:67).
//
// It takes the span between the first quote and the last, not the first pair:
// the rules themselves contain quotation marks — `Anything where the only
// "action" is FYI` is one of them — and stopping at the first inner quote would
// print half a rule and make the card look like it had lost the plot.
func suppressionRule(s model.Signal) string {
	open := strings.Index(s.Detail, `"`)
	end := strings.LastIndex(s.Detail, `"`)
	if open < 0 || end <= open {
		return strings.TrimSpace(s.Detail)
	}
	return s.Detail[open+1 : end]
}
