package localfs

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func TestParseProfile(t *testing.T) {
	p, err := parseProfile("profile.md", readFixture(t, "corpus/profile.md"))
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}

	if p.Owner.Name != "Avery Chen" {
		t.Errorf("owner name = %q, want Avery Chen from the H1", p.Owner.Name)
	}
	if p.Owner.Email != "avery@tessera.io" {
		t.Errorf("owner email = %q, want the address in the profile body", p.Owner.Email)
	}

	if len(p.People) != 7 {
		t.Fatalf("parsed %d people, want 7", len(p.People))
	}

	t.Run("priorities", func(t *testing.T) {
		cases := map[string]model.Priority{
			"Sam Park":                    model.P0,
			"Priya Iyer":                  model.P0,
			"Marcus Webb":                 model.P0,
			"Diane Okafor":                model.P1,
			"Ben Schaffer":                model.P0,
			"Recruiters cold-emailing me": model.P4,
		}
		for name, want := range cases {
			person, ok := p.PersonByName(name)
			if !ok {
				t.Errorf("PersonByName(%q) found nobody", name)
				continue
			}
			if person.Priority != want {
				t.Errorf("%s = %s, want %s", name, person.Priority, want)
			}
		}
	})

	t.Run("roles", func(t *testing.T) {
		sam, _ := p.PersonByName("Sam Park")
		if sam.Role != "partner" {
			t.Errorf("Sam's role = %q, want partner", sam.Role)
		}
		if !strings.Contains(sam.Note, "Personal") {
			t.Errorf("Sam's note = %q, want the sentence after the dash", sam.Note)
		}
	})

	// "P0 during the raise, P2 otherwise" keeps both. Flattening it to one
	// number would lose the condition while looking precise.
	t.Run("conditional priority", func(t *testing.T) {
		ben, ok := p.PersonByName("Ben Schaffer")
		if !ok {
			t.Fatalf("Ben Schaffer missing")
		}
		if !ben.Conditional {
			t.Errorf("Ben not marked conditional: %+v", ben)
		}
		if len(ben.Priorities) != 2 || ben.Priorities[0] != model.P0 || ben.Priorities[1] != model.P2 {
			t.Errorf("Ben's priorities = %v, want [P0 P2]", ben.Priorities)
		}
	})

	// The wrapped bullets in the appendix are one entry each, not two.
	t.Run("wrapped bullets stay one entry", func(t *testing.T) {
		priya, ok := p.PersonByName("Priya Iyer")
		if !ok {
			t.Fatalf("Priya Iyer missing")
		}
		if !strings.Contains(priya.Note, "intentional and important") {
			t.Errorf("Priya's note = %q, want the wrapped continuation joined on", priya.Note)
		}
		group, ok := p.PersonByName("The procurement leads at Halberd Manufacturing, Northstar Foods, and Veritas Components")
		if !ok {
			t.Fatalf("the wrapped group entry did not survive as one person")
		}
		if group.Priority != model.P1 {
			t.Errorf("group entry priority = %s, want P1", group.Priority)
		}
	})

	t.Run("suppressions and tone", func(t *testing.T) {
		if len(p.Suppressions) != 5 {
			t.Errorf("parsed %d suppression rules, want 5: %+v", len(p.Suppressions), p.Suppressions)
		}
		if !strings.HasPrefix(p.Suppressions[0].Text, "Newsletters") {
			t.Errorf("first suppression = %q, want the newsletters rule", p.Suppressions[0].Text)
		}
		if p.Suppressions[0].Line == 0 {
			t.Errorf("suppression carries no line; rules must be citable back to profile.md")
		}
		if len(p.ToneRules) != 4 {
			t.Errorf("parsed %d tone rules, want 4: %+v", len(p.ToneRules), p.ToneRules)
		}
		if !strings.Contains(p.ToneRules[3].Text, "don't draft") {
			t.Errorf("last tone rule = %q, want the Sam rule", p.ToneRules[3].Text)
		}
	})

	t.Run("optional sections", func(t *testing.T) {
		if len(p.HonestyRules) != 3 {
			t.Errorf("parsed %d honesty rules, want 3", len(p.HonestyRules))
		}
		if len(p.MissRules) != 2 {
			t.Errorf("parsed %d miss rules, want 2", len(p.MissRules))
		}
	})

	// Sections the parser does not interpret are still kept: the profile is the
	// user's document, not our schema.
	t.Run("every section is kept", func(t *testing.T) {
		if len(p.Sections) != 7 {
			t.Fatalf("kept %d sections, want 7", len(p.Sections))
		}
		if _, ok := p.SectionByTitle("today"); !ok {
			t.Errorf(`the "What "today" means" section was dropped`)
		}
		if s, ok := p.SectionByTitle("Who I am"); !ok || !strings.Contains(s.Body, "Tessera") {
			t.Errorf("the Who I am section lost its body")
		}
	})
}

func TestParseProfileRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		substr string
	}{
		{"person with no priority", "malformed/profile-no-priority.md", "carries no priority"},
		{"no suppression section", "malformed/profile-no-suppressions.md", "want surfaced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseProfile("profile.md", readFixture(t, tc.file))
			if err == nil {
				t.Fatalf("want an error")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.substr)
			}
		})
	}
}

func TestParseProfileRequiresATitle(t *testing.T) {
	_, err := parseProfile("profile.md", []byte("## People who matter\n\n- **Sam** — P0\n"))
	wantValidationError(t, err, 1, "no \"# \" title")
}

// A bullet with no bold name is not an entry we can rank anyone by.
func TestParseProfileRejectsNamelessPerson(t *testing.T) {
	body := "# A — Profile\n\n## People who matter\n\n- someone important, P0\n\n## What I don't want surfaced\n\n- Newsletters.\n\n## Tone\n\n- Short.\n"
	_, err := parseProfile("profile.md", []byte(body))
	wantValidationError(t, err, 5, "names nobody")
}

// PersonByName matches a full name inside a longer string, but never a bare
// first name: mis-prioritising someone's morning on a substring is worse than
// not matching.
func TestPersonByNameMatching(t *testing.T) {
	p, err := parseProfile("profile.md", readFixture(t, "corpus/profile.md"))
	if err != nil {
		t.Fatalf("parseProfile: %v", err)
	}
	if _, ok := p.PersonByName("Sam Park <sam@parkhouse.example>"); !ok {
		t.Errorf("PersonByName did not match a name inside an addressed string")
	}
	if _, ok := p.PersonByName("sam park"); !ok {
		t.Errorf("PersonByName is case-sensitive")
	}
	if _, ok := p.PersonByName("Sam"); ok {
		t.Errorf("PersonByName matched a bare first name")
	}
	if _, ok := p.PersonByName(""); ok {
		t.Errorf("PersonByName matched the empty string")
	}
}
