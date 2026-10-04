package barrier

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxBarriers, MaxPending, MaxNameBytes int }
type Config struct {
	Name    string
	Parties int
}
type OpKind uint8

const (
	Arrive OpKind = iota + 1
	Cancel
)

type Op struct {
	Kind                 OpKind
	Barrier, Participant string
}
type Batch struct{ Ops []Op }
type Completion struct {
	Barrier      string
	Generation   uint64
	Participants []string
}
type Result struct {
	Generation  uint64
	Completions []Completion
}
type BarrierState struct {
	Name       string
	Parties    int
	Generation uint64
	Pending    []string
}
type Snapshot struct {
	Generation uint64
	Pending    int
	Barriers   []BarrierState
}

type barrierState struct {
	parties    int
	generation uint64
	pending    map[string]struct{}
}

type Registry struct {
	mu       sync.Mutex
	maxPend  int
	maxName  int
	pending  int
	gen      uint64
	barriers map[string]*barrierState
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func New(o Options, cfgs []Config) (*Registry, error) {
	if o.MaxBarriers <= 0 || o.MaxPending <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if len(cfgs) == 0 || len(cfgs) > o.MaxBarriers {
		return nil, ErrInvalidOptions
	}
	r := &Registry{maxPend: o.MaxPending, maxName: o.MaxNameBytes, barriers: make(map[string]*barrierState, len(cfgs))}
	for _, c := range cfgs {
		if !validName(c.Name, o.MaxNameBytes) || c.Parties <= 0 {
			return nil, ErrInvalidOptions
		}
		if _, dup := r.barriers[c.Name]; dup {
			return nil, ErrInvalidOptions
		}
		r.barriers[c.Name] = &barrierState{parties: c.Parties, generation: 1, pending: map[string]struct{}{}}
	}
	return r, nil
}

func (r *Registry) Apply(b Batch) (Result, error) {
	// Phase 1: structural validation only, no state reads.
	for _, op := range b.Ops {
		if op.Kind != Arrive && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Barrier, r.maxName) || !validName(op.Participant, r.maxName) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Phase 2: execute against cloned candidate state.
	type cand struct {
		parties int
		gen     uint64
		pending map[string]struct{}
	}
	cands := make(map[string]*cand, len(r.barriers))
	for name, bs := range r.barriers {
		p := make(map[string]struct{}, len(bs.pending))
		for k := range bs.pending {
			p[k] = struct{}{}
		}
		cands[name] = &cand{parties: bs.parties, gen: bs.generation, pending: p}
	}
	pending := r.pending
	var completions []Completion

	for _, op := range b.Ops {
		c, ok := cands[op.Barrier]
		if !ok {
			return Result{}, ErrNotFound
		}
		switch op.Kind {
		case Arrive:
			if _, dup := c.pending[op.Participant]; dup {
				return Result{}, ErrConflict
			}
			c.pending[op.Participant] = struct{}{}
			pending++
			if len(c.pending) == c.parties {
				ps := make([]string, 0, len(c.pending))
				for p := range c.pending {
					ps = append(ps, p)
				}
				sort.Strings(ps)
				completions = append(completions, Completion{Barrier: op.Barrier, Generation: c.gen, Participants: ps})
				pending -= len(c.pending)
				c.pending = make(map[string]struct{})
				c.gen++
			}
		case Cancel:
			if _, ok := c.pending[op.Participant]; !ok {
				return Result{}, ErrNotFound
			}
			delete(c.pending, op.Participant)
			pending--
		}
	}
	if pending > r.maxPend {
		return Result{}, ErrCapacity
	}

	// Commit candidate state.
	for name, c := range cands {
		bs := r.barriers[name]
		bs.generation = c.gen
		bs.pending = c.pending
	}
	r.pending = pending
	if len(b.Ops) > 0 {
		r.gen++
	}
	return Result{Generation: r.gen, Completions: completions}, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{Generation: r.gen, Pending: r.pending}
	names := make([]string, 0, len(r.barriers))
	for n := range r.barriers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		bs := r.barriers[n]
		ps := make([]string, 0, len(bs.pending))
		for p := range bs.pending {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		s.Barriers = append(s.Barriers, BarrierState{Name: n, Parties: bs.parties, Generation: bs.generation, Pending: ps})
	}
	return s
}
