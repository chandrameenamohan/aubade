package localfs

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// tasks.md is a markdown checklist:
//
//	# Tasks
//	- [ ] Send Diane the Q1 board update (due: 2026-08-28) (id: t-board-update)
//	- [x] Sign Mei Tanaka's offer letter (due: 2026-08-25)
//	- [ ] Re-run the inference cost model
//
// Trailing "(key: value)" groups carry metadata: `due` (YYYY-MM-DD, optionally
// with HH:MM, or RFC3339), `id`, `owner`. Unknown keys land in Task.Meta rather
// than failing the load. Prose and headings between items are ignored — a tasks
// file is allowed to have a title.
//
// What is rejected: a bullet that looks like a checkbox but is not one
// ("- [y] …"), an unparseable due date, and a repeated id. The first of those
// matters most: a typo'd checkbox that parses as prose is a task that silently
// stops existing, which is precisely the failure the digest is supposed to
// catch in Avery's inbox — we should not commit it in our own loader.

var (
	taskItemRE = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s*(.*)$`)
	taskBoxRE  = regexp.MustCompile(`^\s*[-*]\s+\[`)
	taskMetaRE = regexp.MustCompile(`\s*\(([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*([^()]*)\)\s*$`)
)

// parseTasks reads tasks.md into tasks, in file order.
func parseTasks(path string, data []byte, loc *time.Location) ([]model.Task, error) {
	fail := func(line int, msg string, err error) error {
		return &model.ValidationError{
			Source: string(model.SourceTask), Path: path, Line: line, Msg: msg, Err: err,
		}
	}

	var (
		tasks []model.Task
		seen  = map[string]int{}
	)
	for i, raw := range splitLines(string(data)) {
		lineNo := i + 1

		m := taskItemRE.FindStringSubmatch(raw)
		if m == nil {
			if taskBoxRE.MatchString(raw) {
				return nil, fail(lineNo, fmt.Sprintf("%q is not a task item; want \"- [ ] title\" or \"- [x] title\"", strings.TrimSpace(raw)), nil)
			}
			continue
		}

		task := model.Task{
			Done: m[1] == "x" || m[1] == "X",
			Line: lineNo,
		}

		title, meta := stripTaskMeta(m[2])
		task.Title = strings.TrimSpace(title)
		if task.Title == "" {
			return nil, fail(lineNo, "task has no title", nil)
		}

		for key, value := range meta {
			switch key {
			case "due":
				t, err := parseMarkdownDate(value, loc)
				if err != nil {
					return nil, fail(lineNo, fmt.Sprintf("due date %q", value), err)
				}
				task.Due = t
			case "id":
				task.ID = value
			case "owner":
				task.Owner = value
			default:
				if task.Meta == nil {
					task.Meta = map[string]string{}
				}
				task.Meta[key] = value
			}
		}

		if task.ID == "" {
			task.ID = fmt.Sprintf("task-%d", len(tasks)+1)
		}
		if first, dup := seen[task.ID]; dup {
			return nil, fail(lineNo, fmt.Sprintf("duplicate task id %q (first seen on line %d)", task.ID, first), nil)
		}
		seen[task.ID] = lineNo
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// stripTaskMeta peels trailing "(key: value)" groups off a task line and
// returns the remaining title with the metadata it found. Later groups win over
// earlier ones only when a key repeats, which is not a shape we write.
func stripTaskMeta(s string) (string, map[string]string) {
	meta := map[string]string{}
	for {
		m := taskMetaRE.FindStringSubmatchIndex(s)
		if m == nil {
			return s, meta
		}
		key := strings.ToLower(strings.TrimSpace(s[m[2]:m[3]]))
		value := strings.TrimSpace(s[m[4]:m[5]])
		if _, dup := meta[key]; !dup {
			meta[key] = value
		}
		s = s[:m[0]]
	}
}
