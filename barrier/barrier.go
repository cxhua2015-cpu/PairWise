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
	mu         sync.Mutex
	maxPend    int
	maxName    int
	generation uint64
	barriers   map[string]*barrierState
	order      []string // barrier names sorted
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func New(o Options, cfgs []Config) (*Registry, error) {
	if o.MaxBarriers <= 0 || o.MaxPending <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if len(cfgs) < 1 || len(cfgs) > o.MaxBarriers {
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
		r.order = append(r.order, c.Name)
	}
	sort.Strings(r.order)
	return r, nil
}

func (r *Registry) Apply(b Batch) (Result, error) {
	// Phase 1: structural validation only, no state reads.
	for _, op := range b.Ops {
		if op.Kind != Arrive && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, op := range b.Ops {
		if !validName(op.Barrier, r.maxName) || !validName(op.Participant, r.maxName) {
			return Result{}, ErrInvalidInput
		}
	}
	// Phase 2: execute on cloned candidate state.
	cand := make(map[string]*barrierState, len(r.barriers))
	for name, bs := range r.barriers {
		cp := &barrierState{parties: bs.parties, generation: bs.generation, pending: make(map[string]struct{}, len(bs.pending))}
		for p := range bs.pending {
			cp.pending[p] = struct{}{}
		}
		cand[name] = cp
	}
	var comps []Completion
	total := 0
	for _, bs := range r.barriers {
		total += len(bs.pending)
	}
	for _, op := range b.Ops {
		bs, ok := cand[op.Barrier]
		if !ok {
			return Result{}, ErrNotFound
		}
		switch op.Kind {
		case Arrive:
			if _, dup := bs.pending[op.Participant]; dup {
				return Result{}, ErrConflict
			}
			bs.pending[op.Participant] = struct{}{}
			total++
			if len(bs.pending) == bs.parties {
				ps := make([]string, 0, len(bs.pending))
				for p := range bs.pending {
					ps = append(ps, p)
				}
				sort.Strings(ps)
				comps = append(comps, Completion{Barrier: op.Barrier, Generation: bs.generation, Participants: ps})
				bs.pending = make(map[string]struct{}, bs.parties)
				total -= bs.parties
				bs.generation++
			}
		case Cancel:
			if _, ok := bs.pending[op.Participant]; !ok {
				return Result{}, ErrNotFound
			}
			delete(bs.pending, op.Participant)
			total--
		}
	}
	if total > r.maxPend {
		return Result{}, ErrCapacity
	}
	// Commit.
	for name, bs := range cand {
		r.barriers[name] = bs
	}
	if len(b.Ops) > 0 {
		r.generation++
	}
	return Result{Generation: r.generation, Completions: comps}, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{Generation: r.generation, Barriers: make([]BarrierState, 0, len(r.order))}
	for _, name := range r.order {
		bs := r.barriers[name]
		ps := make([]string, 0, len(bs.pending))
		for p := range bs.pending {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		s.Pending += len(ps)
		s.Barriers = append(s.Barriers, BarrierState{Name: name, Parties: bs.parties, Generation: bs.generation, Pending: ps})
	}
	return s
}
