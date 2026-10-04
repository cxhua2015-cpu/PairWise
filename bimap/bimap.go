package bimap

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrMismatch       = errors.New("pair mismatch")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxPairs, MaxNameBytes int }

type OpKind uint8

const (
	Bind OpKind = iota + 1
	Unbind
)

type Op struct {
	Kind        OpKind
	Left, Right string
}

type Batch struct{ Ops []Op }

type Pair struct {
	Left, Right string
	Revision    uint64
}

type Result struct {
	Generation, Revision uint64
	Changed              []Pair
}

type Snapshot struct {
	Generation, NextRevision uint64
	Pairs                    []Pair
}

type Registry struct {
	mu         sync.Mutex
	maxPairs   int
	maxNameLen int
	l2r        map[string]string
	r2l        map[string]string
	rev        map[string]uint64
	generation uint64
	revision   uint64
}

func New(o Options) (*Registry, error) {
	if o.MaxPairs <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		maxPairs:   o.MaxPairs,
		maxNameLen: o.MaxNameBytes,
		l2r:        make(map[string]string),
		r2l:        make(map[string]string),
		rev:        make(map[string]uint64),
	}, nil
}

func (r *Registry) validName(s string) bool {
	if s == "" || len(s) > r.maxNameLen {
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

func (r *Registry) Apply(b Batch) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, op := range b.Ops {
		if op.Kind != Bind && op.Kind != Unbind {
			return Result{}, ErrInvalidInput
		}
		if !r.validName(op.Left) || !r.validName(op.Right) {
			return Result{}, ErrInvalidInput
		}
	}

	l2r := make(map[string]string, len(r.l2r))
	r2l := make(map[string]string, len(r.r2l))
	rev := make(map[string]uint64, len(r.rev))
	for k, v := range r.l2r {
		l2r[k] = v
	}
	for k, v := range r.r2l {
		r2l[k] = v
	}
	for k, v := range r.rev {
		rev[k] = v
	}
	revision := r.revision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		touched[op.Left] = struct{}{}
		if op.Kind == Bind {
			if _, ok := l2r[op.Left]; ok {
				return Result{}, ErrConflict
			}
			if _, ok := r2l[op.Right]; ok {
				return Result{}, ErrConflict
			}
			revision++
			l2r[op.Left] = op.Right
			r2l[op.Right] = op.Left
			rev[op.Left] = revision
		} else {
			right, ok := l2r[op.Left]
			if !ok {
				if _, ok2 := r2l[op.Right]; ok2 {
					return Result{}, ErrMismatch
				}
				return Result{}, ErrNotFound
			}
			if right != op.Right {
				return Result{}, ErrMismatch
			}
			delete(l2r, op.Left)
			delete(r2l, op.Right)
			delete(rev, op.Left)
		}
	}

	if len(l2r) > r.maxPairs {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		r.generation++
	}
	r.l2r, r.r2l, r.rev = l2r, r2l, rev
	r.revision = revision

	var changed []Pair
	for left := range touched {
		if right, ok := l2r[left]; ok {
			changed = append(changed, Pair{Left: left, Right: right, Revision: rev[left]})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Left < changed[j].Left })

	return Result{Generation: r.generation, Revision: revision, Changed: changed}, nil
}

func (r *Registry) LookupLeft(left string) (Pair, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.validName(left) {
		return Pair{}, false, ErrInvalidInput
	}
	right, ok := r.l2r[left]
	if !ok {
		return Pair{}, false, nil
	}
	return Pair{Left: left, Right: right, Revision: r.rev[left]}, true, nil
}

func (r *Registry) LookupRight(right string) (Pair, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.validName(right) {
		return Pair{}, false, ErrInvalidInput
	}
	left, ok := r.r2l[right]
	if !ok {
		return Pair{}, false, nil
	}
	return Pair{Left: left, Right: right, Revision: r.rev[left]}, true, nil
}

func (r *Registry) List(after string, limit int) ([]Pair, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if after != "" && !r.validName(after) {
		return nil, ErrInvalidInput
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	pairs := r.sortedPairs()
	out := make([]Pair, 0, limit)
	for _, p := range pairs {
		if p.Left <= after {
			continue
		}
		out = append(out, p)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{
		Generation:   r.generation,
		NextRevision: r.revision + 1,
		Pairs:        r.sortedPairs(),
	}
}

func (r *Registry) sortedPairs() []Pair {
	pairs := make([]Pair, 0, len(r.l2r))
	for left, right := range r.l2r {
		pairs = append(pairs, Pair{Left: left, Right: right, Revision: r.rev[left]})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Left < pairs[j].Left })
	return pairs
}
