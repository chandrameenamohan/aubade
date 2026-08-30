package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// The registry is what makes the engine ignorant of vendors. A runner is a name
// and a constructor; `--runner=<name>` and the consensus roster both resolve
// through here, so adding an SDK-backed runner later is a Register call and
// nothing else.
//
// gemini is deliberately NOT registered. It is absent from the development
// machine under every name we looked for, so its headless syntax has never been
// verified against the real binary (learning-tests/05) — and an unprobed runner
// is not a voter. Registering it would put a guess into a majority vote, which
// is worse than having one fewer voter.

// Registry maps runner names to constructors.
type Registry struct {
	mu    sync.RWMutex
	order []string
	make  map[string]func() Runner
}

// NewRegistry builds an empty registry. Tests use this to vote with fakes; the
// product uses Default.
func NewRegistry() *Registry {
	return &Registry{make: map[string]func() Runner{}}
}

// Register adds a constructor under name, replacing any previous one.
func (r *Registry) Register(name string, f func() Runner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, seen := r.make[name]; !seen {
		r.order = append(r.order, name)
	}
	r.make[name] = f
}

// Names lists registered runners in registration order — which is also the
// order the footer names them in, so two runs never disagree about who voted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// New builds the named runner. An unknown name lists the alternatives, because
// the caller mistyped a flag and the menu is the whole answer.
func (r *Registry) New(name string) (Runner, error) {
	r.mu.RLock()
	f, ok := r.make[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown runner %q; one of: %s", name, strings.Join(r.Names(), ", "))
	}
	return f(), nil
}

// defaultRegistry holds the runners this build ships with.
var defaultRegistry = func() *Registry {
	r := NewRegistry()
	r.Register("claude", func() Runner { return NewClaude() })
	r.Register("codex", func() Runner { return NewCodex() })
	return r
}()

// Default is the registry the product uses.
func Default() *Registry { return defaultRegistry }

// State is what detection concluded about one runner.
type State string

const (
	// Live means a real capped call came back. This is the only state that
	// votes.
	Live State = "live"
	// Dead means the binary is there and could not answer — a 401, a timeout, a
	// broken install. A dead runner is dropped from the tally rather than
	// counted as a dissent: a 401 is not an opinion.
	Dead State = "dead"
	// Absent means there is no binary at all.
	Absent State = "absent"
)

// Status is one runner's detection result. Detail carries the reason a runner is
// dead, because the digest footer owes the reader that fact rather than a
// shorter list of voters.
type Status struct {
	Name   string
	State  State
	Detail string
}

// Roster is who can vote this morning, and what happened to everyone else.
type Roster struct {
	Statuses []Status
	live     []Runner
}

// Live returns the runners that answered a probe, in registration order.
func (r *Roster) Live() []Runner { return append([]Runner(nil), r.live...) }

// LiveNames is the bare list of runners that answered. The digest footer wants
// Describe() instead — it owes the reader the dead and absent ones too — so this
// is the short form, for a caller that only needs who voted.
func (r *Roster) LiveNames() []string {
	out := make([]string, 0, len(r.live))
	for _, x := range r.live {
		out = append(out, x.Name())
	}
	return out
}

// Get returns the live runner with this name.
func (r *Roster) Get(name string) (Runner, bool) {
	for _, x := range r.live {
		if x.Name() == name {
			return x, true
		}
	}
	return nil, false
}

// StatusOf returns what detection concluded about one runner.
func (r *Roster) StatusOf(name string) (Status, bool) {
	for _, s := range r.Statuses {
		if s.Name == name {
			return s, true
		}
	}
	return Status{}, false
}

// Describe renders the roster the way the digest footer states it: who voted,
// who could not, and who was never here.
func (r *Roster) Describe() string {
	var live, dead, absent []string
	for _, s := range r.Statuses {
		switch s.State {
		case Live:
			live = append(live, s.Name)
		case Dead:
			dead = append(dead, fmt.Sprintf("%s unavailable (%s)", s.Name, s.Detail))
		case Absent:
			absent = append(absent, s.Name+" not installed")
		}
	}
	parts := make([]string, 0, 3)
	if len(live) > 0 {
		parts = append(parts, "answered: "+strings.Join(live, ", "))
	} else {
		parts = append(parts, "answered: none")
	}
	parts = append(parts, dead...)
	parts = append(parts, absent...)
	return strings.Join(parts, "; ")
}

// Detect probes every registered runner in parallel and reports the roster.
//
// It probes rather than trusting PATH because presence is not liveness, and it
// probes in parallel because the probes are wall-clock bound and serial probing
// would make detection the slowest part of a digest.
func (reg *Registry) Detect(ctx context.Context) *Roster {
	names := reg.Names()
	statuses := make([]Status, len(names))
	runners := make([]Runner, len(names))

	var wg sync.WaitGroup
	for i, name := range names {
		x, err := reg.New(name)
		if err != nil {
			statuses[i] = Status{Name: name, State: Absent, Detail: err.Error()}
			continue
		}
		if !x.Installed() {
			statuses[i] = Status{Name: name, State: Absent}
			continue
		}
		wg.Add(1)
		go func(i int, x Runner) {
			defer wg.Done()
			if err := x.Probe(ctx); err != nil {
				statuses[i] = Status{Name: x.Name(), State: Dead, Detail: shortReason(err)}
				return
			}
			statuses[i] = Status{Name: x.Name(), State: Live}
			runners[i] = x
		}(i, x)
	}
	wg.Wait()

	roster := &Roster{Statuses: statuses}
	for _, x := range runners {
		if x != nil {
			roster.live = append(roster.live, x)
		}
	}
	return roster
}

// shortReason compresses a runner failure — a probe that did not answer, or a
// vote that errored — to the one line a footer can carry.
func shortReason(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.Index(msg, ": "); i > 0 && i < 40 {
		msg = msg[i+2:]
	}
	if len(msg) > 120 {
		msg = msg[:120] + "…"
	}
	return msg
}
