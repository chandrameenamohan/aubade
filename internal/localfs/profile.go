package localfs

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// profile.md is the user's own document, written in prose, and the parser reads
// it the way a person does: an "# H1" naming the owner, "## " sections, and
// bullets under them. Markdown lazy continuation is honoured — a bullet that
// wraps onto the next unindented line is still one bullet — because the
// appendix profile wraps constantly and a parser that split on newlines would
// turn "Ben Schaffer … P0 during the raise, P2 / otherwise" into two entries.
//
// Three sections are load-bearing and required: who matters and at what
// priority, what must never be surfaced, and how a drafted reply should sound.
// Their absence is a validation error rather than an empty slice: a digest that
// silently forgot the suppression list would cheerfully surface the newsletters
// the user explicitly banned, and nothing downstream would know to complain.
// Every other section is kept verbatim in Profile.Sections.

// profileSource labels profile.md in validation errors. It is deliberately not
// a model.Source: citations point at data, never at settings.
const profileSource = "profile"

var (
	profileH1RE     = regexp.MustCompile(`^#\s+(.+?)\s*$`)
	profileH2RE     = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	profileBulletRE = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)
	profilePersonRE = regexp.MustCompile(`^\*\*(.+?)\*\*\s*(?:\(([^)]*)\))?\s*(?:—|–|--|-)?\s*(.*)$`)
	profilePriorRE  = regexp.MustCompile(`\bP[0-4]\b`)
	profileEmailRE  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// parseProfile reads profile.md.
func parseProfile(path string, data []byte) (*model.Profile, error) {
	fail := func(line int, msg string) error {
		return &model.ValidationError{Source: profileSource, Path: path, Line: line, Msg: msg}
	}

	lines := splitLines(string(data))
	profile := &model.Profile{Path: path}

	// Owner: the H1 title, minus a trailing "— Profile" the user wrote for
	// their own benefit.
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			break
		}
		if m := profileH1RE.FindStringSubmatch(l); m != nil {
			profile.Owner.Name = trimProfileSuffix(m[1])
			break
		}
	}
	if profile.Owner.Name == "" {
		return nil, fail(1, `no "# " title: the first heading names whose profile this is`)
	}
	if addr := profileEmailRE.FindString(string(data)); addr != "" {
		profile.Owner.Email = addr
	}

	profile.Sections = parseProfileSections(lines)

	people, err := findProfileSection(profile.Sections, "people who matter")
	if err != nil {
		return nil, fail(0, err.Error())
	}
	profile.People, err = parseProfilePeople(path, people.Bullets)
	if err != nil {
		return nil, err
	}
	if len(profile.People) == 0 {
		return nil, fail(people.Line, `section "`+people.Title+`" lists nobody`)
	}

	suppressions, err := findProfileSection(profile.Sections, "want surfaced")
	if err != nil {
		return nil, fail(0, err.Error())
	}
	profile.Suppressions = suppressions.Bullets

	tone, err := findProfileSection(profile.Sections, "tone")
	if err != nil {
		return nil, fail(0, err.Error())
	}
	profile.ToneRules = tone.Bullets

	// Optional, but the honesty layer reads them when they are there.
	if s, err := findProfileSection(profile.Sections, "honest"); err == nil {
		profile.HonestyRules = s.Bullets
	}
	if s, err := findProfileSection(profile.Sections, "might miss"); err == nil {
		profile.MissRules = s.Bullets
	}

	return profile, nil
}

// parseProfileSections splits the document into its "## " sections, collecting
// each one's raw body and its bullets.
func parseProfileSections(lines []string) []model.ProfileSection {
	var (
		sections []model.ProfileSection
		cur      *model.ProfileSection
		body     []string
	)

	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
		sections = append(sections, *cur)
		cur, body = nil, nil
	}

	for i, l := range lines {
		if m := profileH2RE.FindStringSubmatch(l); m != nil {
			flush()
			cur = &model.ProfileSection{Title: strings.TrimSpace(m[1]), Line: i + 1}
			continue
		}
		if cur == nil {
			continue
		}
		body = append(body, l)

		if m := profileBulletRE.FindStringSubmatch(l); m != nil {
			cur.Bullets = append(cur.Bullets, model.Rule{Text: strings.TrimSpace(m[1]), Line: i + 1})
			continue
		}
		// Markdown lazy continuation: a non-blank, non-heading line under an
		// open bullet belongs to that bullet.
		if n := len(cur.Bullets); n > 0 && strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			cur.Bullets[n-1].Text += " " + strings.TrimSpace(l)
		}
	}
	flush()
	return sections
}

// findProfileSection locates a required section by a distinctive fragment of
// its heading. Headings are prose the user wrote, so matching is loose on case
// and punctuation and strict about nothing else.
func findProfileSection(sections []model.ProfileSection, fragment string) (*model.ProfileSection, error) {
	for i := range sections {
		if strings.Contains(normalizeHeading(sections[i].Title), fragment) {
			return &sections[i], nil
		}
	}
	return nil, fmt.Errorf("no \"## \" section matching %q; profile.md must say who matters, what to suppress, and how to sound", fragment)
}

// normalizeHeading lowercases a heading and drops the punctuation that varies
// between a straight and a curly apostrophe ("don't" / "don’t").
func normalizeHeading(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case '\'', '‘', '’', '"', '“', '”':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// trimProfileSuffix drops the "— Profile" the H1 usually carries.
func trimProfileSuffix(title string) string {
	for _, sep := range []string{"—", "–", " - ", "--"} {
		if i := strings.Index(title, sep); i > 0 {
			tail := normalizeHeading(title[i+len(sep):])
			if tail == "profile" {
				return strings.TrimSpace(title[:i])
			}
		}
	}
	return strings.TrimSpace(title)
}

// parseProfilePeople turns "People who matter" bullets into entries.
//
// Every entry must name someone in bold and carry a P0..P4 — those two are the
// contract the ranking reads. An entry that mentions more than one priority
// ("P0 during the raise, P2 otherwise") keeps all of them and is marked
// conditional, so the orchestrator can read the condition rather than have the
// loader pretend it resolved it.
func parseProfilePeople(path string, bullets []model.Rule) ([]model.ProfilePerson, error) {
	people := make([]model.ProfilePerson, 0, len(bullets))
	for _, b := range bullets {
		fail := func(msg string) error {
			return &model.ValidationError{Source: profileSource, Path: path, Line: b.Line, Msg: msg}
		}

		m := profilePersonRE.FindStringSubmatch(b.Text)
		if m == nil {
			return nil, fail(fmt.Sprintf("%q names nobody; want \"- **Name** (role) — … P0\"", b.Text))
		}

		var priorities []model.Priority
		for _, raw := range profilePriorRE.FindAllString(b.Text, -1) {
			p, err := model.ParsePriority(raw)
			if err != nil {
				return nil, fail(err.Error())
			}
			if !slices.Contains(priorities, p) {
				priorities = append(priorities, p)
			}
		}
		if len(priorities) == 0 {
			return nil, fail(fmt.Sprintf("%q carries no priority; want one of P0, P1, P2, P3, P4", b.Text))
		}

		people = append(people, model.ProfilePerson{
			Name:        strings.TrimSpace(m[1]),
			Role:        strings.TrimSpace(m[2]),
			Priority:    priorities[0],
			Priorities:  priorities,
			Conditional: len(priorities) > 1,
			Note:        strings.TrimSpace(m[3]),
			Line:        b.Line,
		})
	}
	return people, nil
}
