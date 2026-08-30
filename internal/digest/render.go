package digest

import (
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/styles"
)

// Rendering is deliberately the dumbest layer here. Every interesting decision —
// what is on the page, in what order, with what receipts — was made by the time
// a Digest exists, and this file turns that struct into markdown without
// consulting the corpus again. That split is what makes a customized format
// (SPEC §6) safe to build later: a different renderer over the same Digest
// cannot add, drop, or re-rank a fact.
//
// The markdown itself mirrors the sample digest in the assignment PDF: an H1
// with the date, a timezone line, H2 sections in a fixed order, bold leads, and
// a citation span closing each factual line.

// Markdown renders the page.
func (d *Digest) Markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Daily Digest — %s\n", d.Day.Format("Monday, January 2, 2006"))
	fmt.Fprintf(&b, "Current Timezone: %s.\n", d.Loc.String())
	if who := strings.TrimSpace(d.Owner.Name); who != "" {
		fmt.Fprintf(&b, "For %s, written at 06:00.\n", who)
	}
	b.WriteString("\n")

	d.writeBanner(&b)

	for _, s := range d.Sections {
		d.writeSection(&b, s)
	}

	d.writeFooter(&b)
	return b.String()
}

// writeBanner opens the page with what the reader needs before trusting
// anything below it. A blockquote rather than a section: it is not one of the
// digest's five answers, it is the caveat on all of them.
func (d *Digest) writeBanner(b *strings.Builder) {
	if len(d.Banner) == 0 {
		return
	}
	for _, it := range d.Banner {
		fmt.Fprintf(b, "> **Heads up — %s** %s%s\n>\n", it.Lead, it.Body, space(it.CiteSpan()))
	}
	b.WriteString("> These lines are the honesty layer, not a formatting choice: what follows is only as good as the sources above.\n\n")
}

// writeSection renders one heading and its contents.
func (d *Digest) writeSection(b *strings.Builder, s Section) {
	if len(s.Items) == 0 && s.Empty == "" {
		return // "I'm not sure" with nothing in it is not a finding.
	}

	fmt.Fprintf(b, "## %s\n", s.Heading)
	switch {
	case len(s.Items) == 0:
		fmt.Fprintf(b, "%s\n", s.Empty)
	case s.Paragraph:
		for _, it := range s.Items {
			fmt.Fprintf(b, "**%s** %s%s\n", it.Lead, it.Body, space(it.CiteSpan()))
			d.writeItemExtras(b, it, "")
		}
	default:
		for _, it := range s.Items {
			fmt.Fprintf(b, "- **%s** %s%s\n", it.Lead, it.Body, space(it.CiteSpan()))
			d.writeItemExtras(b, it, "  ")
		}
	}
	if s.Overflow > 0 {
		fmt.Fprintf(b, "- *%s ranked below the fold and not shown; `aubade signals` has the full set.*\n",
			plural(s.Overflow, "more item", "more items"))
	}
	b.WriteString("\n")
}

// writeItemExtras renders what hangs off a line: the two sides of a
// disagreement, and a reply ready to finish.
func (d *Digest) writeItemExtras(b *strings.Builder, it Item, indent string) {
	for _, side := range it.Sides {
		fmt.Fprintf(b, "%s- %s%s\n", indent, side.Text, space(citeSpan([]string{side.Ref})))
	}
	if it.Draft != nil {
		d.writeDraft(b, it.Draft, indent)
	}
}

// writeDraft renders a dispatchable's reply, or the rule that stopped it.
func (d *Digest) writeDraft(b *strings.Builder, dr *Draft, indent string) {
	if dr.Skipped {
		fmt.Fprintf(b, "%s- *Not drafted — %s.*\n", indent, dr.SkipReason)
		return
	}
	if strings.TrimSpace(dr.Body) == "" {
		return
	}

	register := "in your voice"
	if dr.Polished {
		register = "in your voice, investor register"
	}
	fmt.Fprintf(b, "%s- *Draft reply to %s, %s (%s). The answer is yours; aubade will not invent it.*\n",
		indent, dr.To.String(), register, voiceProvenance(dr.Rules))
	fmt.Fprintf(b, "%s  ```text\n", indent)
	fmt.Fprintf(b, "%s  Subject: %s\n%s\n", indent, dr.Subject, indentBlock(dr.Body, indent+"  "))
	fmt.Fprintf(b, "%s  ```\n", indent)
}

// writeFooter says how the page was made and what it was made from. It is part
// of the product, not decoration: a reader who knows no model touched this page
// reads its confidence differently.
func (d *Digest) writeFooter(b *strings.Builder) {
	b.WriteString("---\n")

	sources := "no sources"
	if n := len(d.Stats.Sources); n > 0 {
		sources = fmt.Sprintf("%s (%s)", plural(n, "source", "sources"), strings.Join(d.Stats.Sources, ", "))
	}
	fmt.Fprintf(b, "*Composed by `aubade digest --%s`: %s from %s; extractors run in a fixed order, no model and no network in the loop.",
		d.Mode, plural(d.Stats.Signals, "signal", "signals"), sources)
	if len(d.Stats.Missing) > 0 {
		fmt.Fprintf(b, " Missing: %s.", strings.Join(d.Stats.Missing, ", "))
	}
	if d.Stats.Suppressed > 0 {
		fmt.Fprintf(b, " %s held back by your profile.", plural(d.Stats.Suppressed, "item", "items"))
	}
	if n := d.Voice.ProfileRules(); n > 0 {
		fmt.Fprintf(b, " Voice: %s, overridden by %s (%s applied).", styles.DefaultVoicePath, d.Voice.ProfilePath, plural(n, "rule", "rules"))
	} else {
		fmt.Fprintf(b, " Voice: %s, with no profile tone rules to override it.", styles.DefaultVoicePath)
	}
	b.WriteString(" Every factual line above carries its own citation.*\n")
}

// voiceProvenance names the two layers that produced a draft: aubade's base
// voice, and the user's own lines that overrode it. It is the same kind of
// receipt as a citation — a draft is written in somebody's name, so the rules
// that shaped it should be as checkable as the facts that went into it.
func voiceProvenance(overrides []VoiceRule) string {
	base := "base " + styles.DefaultVoicePath
	if len(overrides) == 0 {
		return base + ", no profile tone rules"
	}
	lines := make([]string, 0, len(overrides))
	for _, r := range overrides {
		lines = append(lines, itoa(r.Line))
	}
	return fmt.Sprintf("%s, overridden by %s:%s", base, overrides[0].Path, strings.Join(lines, ","))
}

// space prefixes a non-empty citation span with the one space it needs at the
// end of a sentence. An uncited line renders bare — and there should be almost
// none: the only lines this page writes without a receipt are the ones it
// authored itself, like an empty-section sentence.
func space(span string) string {
	if span == "" {
		return ""
	}
	return " " + span
}

// indentBlock prefixes every line of a block with indent, leaving blank lines
// blank so a fenced draft does not carry trailing whitespace.
func indentBlock(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
