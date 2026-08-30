package localfs

import (
	"testing"
	"time"
)

func TestParseTasks(t *testing.T) {
	loc := testLoc(t)
	tasks, err := parseTasks("tasks.md", readFixture(t, "corpus/tasks.md"), loc)
	if err != nil {
		t.Fatalf("parseTasks: %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("parsed %d tasks, want 5", len(tasks))
	}

	first := tasks[0]
	if first.ID != "t-cap-table" {
		t.Errorf("id = %q, want the explicit (id: …)", first.ID)
	}
	if first.Title != "Send Marcus the updated cap table" {
		t.Errorf("title = %q, want the metadata stripped off the end", first.Title)
	}
	if want := time.Date(2026, 8, 28, 0, 0, 0, 0, loc); !first.Due.Equal(want) {
		t.Errorf("due = %s, want %s", first.Due, want)
	}
	if first.Done {
		t.Errorf("unchecked task reported done")
	}
	if first.Line != 5 {
		t.Errorf("line = %d, want 5 (the citation points at the file)", first.Line)
	}

	if !tasks[1].Done {
		t.Errorf("[x] task not reported done")
	}
	if tasks[1].ID != "task-2" {
		t.Errorf("id = %q, want the positional fallback task-2", tasks[1].ID)
	}

	third := tasks[2]
	if want := time.Date(2026, 8, 31, 9, 0, 0, 0, loc); !third.Due.Equal(want) {
		t.Errorf("due = %s, want %s from the date-and-time form", third.Due, want)
	}
	if third.Owner != "avery" {
		t.Errorf("owner = %q, want avery", third.Owner)
	}

	if tasks[3].Meta["source"] != "series-a-model" {
		t.Errorf("meta = %v, want the unknown key preserved", tasks[3].Meta)
	}
	if tasks[3].HasDue() {
		t.Errorf("task with no (due: …) reported a due date")
	}

	// "* [ ]" is a checklist item too; markdown allows either bullet marker.
	if tasks[4].Title != "Reply to Renee about the Sept 4 rollout" {
		t.Errorf("title = %q, want the '*' bullet parsed", tasks[4].Title)
	}
}

func TestParseTasksRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		line   int
		substr string
	}{
		// The one that matters: a typo'd checkbox that parses as prose is a
		// task that silently stops existing.
		{"not a checkbox", "malformed/tasks-bad-checkbox.md", 4, "is not a task item"},
		{"unparseable due", "malformed/tasks-bad-due.md", 3, "due date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTasks("tasks.md", readFixture(t, tc.file), testLoc(t))
			wantValidationError(t, err, tc.line, tc.substr)
		})
	}
}

func TestParseTasksRejectsDuplicateID(t *testing.T) {
	body := "- [ ] one (id: t-1)\n- [ ] two (id: t-1)\n"
	_, err := parseTasks("tasks.md", []byte(body), testLoc(t))
	wantValidationError(t, err, 2, "duplicate task id")
}

func TestParseTasksRejectsEmptyTitle(t *testing.T) {
	_, err := parseTasks("tasks.md", []byte("- [ ] (id: t-1)\n"), testLoc(t))
	wantValidationError(t, err, 1, "no title")
}

// Prose and headings around the checklist are normal in a hand-kept file.
func TestParseTasksIgnoresProse(t *testing.T) {
	body := "# Tasks\n\nSome context.\n\n- [ ] the only task\n\nA closing thought.\n"
	tasks, err := parseTasks("tasks.md", []byte(body), testLoc(t))
	if err != nil {
		t.Fatalf("parseTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "the only task" {
		t.Fatalf("parsed %+v, want exactly the one task", tasks)
	}
}
