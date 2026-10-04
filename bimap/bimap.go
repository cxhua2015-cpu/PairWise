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
	mu           sync.RWMutex
	maxPairs     int
	maxNameBytes int
	leftToRight  map[string]string
	rightToLeft  map[string]string
	revisions    map[string]uint64 // by Left
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Registry, error) {
	if o.MaxPairs <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		maxPairs:     o.MaxPairs,
		maxNameBytes: o.MaxNameBytes,
		leftToRight:  make(map[string]string),
		rightToLeft:  make(map[string]string),
		revisions:    make(map[string]uint64),
		nextRevision: 1,
	}, nil
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

func (r *Registry) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Bind && op.Kind != Unbind {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Left, r.maxNameBytes) || !validName(op.Right, r.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	l2r := make(map[string]string, len(r.leftToRight))
	for k, v := range r.leftToRight {
		l2r[k] = v
	}
	r2l := make(map[string]string, len(r.rightToLeft))
	for k, v := range r.rightToLeft {
		r2l[k] = v
	}
	revs := make(map[string]uint64, len(r.revisions))
	for k, v := range r.revisions {
		revs[k] = v
	}
	nextRev := r.nextRevision

	touched := make(map[string]struct{})
	for _, op := range b.Ops {
		switch op.Kind {
		case Bind:
			if _, ok := l2r[op.Left]; ok {
				return Result{}, ErrConflict
			}
			if _, ok := r2l[op.Right]; ok {
				return Result{}, ErrConflict
			}
			l2r[op.Left] = op.Right
			r2l[op.Right] = op.Left
			revs[op.Left] = nextRev
			nextRev++
			touched[op.Left] = struct{}{}
		case Unbind:
			right, ok := l2r[op.Left]
			if !ok {
				if _, ok := r2l[op.Right]; !ok {
					return Result{}, ErrNotFound
				}
				return Result{}, ErrMismatch
			}
			if right != op.Right {
				return Result{}, ErrMismatch
			}
			delete(l2r, op.Left)
			delete(r2l, op.Right)
			delete(revs, op.Left)
			touched[op.Left] = struct{}{}
		}
	}

	if len(l2r) > r.maxPairs {
		return Result{}, ErrCapacity
	}

	r.leftToRight = l2r
	r.rightToLeft = r2l
	r.revisions = revs
	r.nextRevision = nextRev
	if len(b.Ops) > 0 {
		r.generation++
	}

	res := Result{Generation: r.generation}
	if nextRev > 1 {
		res.Revision = nextRev - 1
	}
	for left := range touched {
		if right, ok := l2r[left]; ok {
			res.Changed = append(res.Changed, Pair{Left: left, Right: right, Revision: revs[left]})
		}
	}
	sort.Slice(res.Changed, func(i, j int) bool { return res.Changed[i].Left < res.Changed[j].Left })
	return res, nil
}

func (r *Registry) LookupLeft(left string) (Pair, bool, error) {
	if !validName(left, r.maxNameBytes) {
		return Pair{}, false, ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	right, ok := r.leftToRight[left]
	if !ok {
		return Pair{}, false, nil
	}
	return Pair{Left: left, Right: right, Revision: r.revisions[left]}, true, nil
}

func (r *Registry) LookupRight(right string) (Pair, bool, error) {
	if !validName(right, r.maxNameBytes) {
		return Pair{}, false, ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	left, ok := r.rightToLeft[right]
	if !ok {
		return Pair{}, false, nil
	}
	return Pair{Left: left, Right: right, Revision: r.revisions[left]}, true, nil
}

func (r *Registry) List(after string, limit int) ([]Pair, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	if after != "" && !validName(after, r.maxNameBytes) {
		return nil, ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	lefts := make([]string, 0, len(r.leftToRight))
	for left := range r.leftToRight {
		if left > after {
			lefts = append(lefts, left)
		}
	}
	sort.Strings(lefts)
	if len(lefts) > limit {
		lefts = lefts[:limit]
	}
	pairs := make([]Pair, 0, len(lefts))
	for _, left := range lefts {
		pairs = append(pairs, Pair{Left: left, Right: r.leftToRight[left], Revision: r.revisions[left]})
	}
	return pairs, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lefts := make([]string, 0, len(r.leftToRight))
	for left := range r.leftToRight {
		lefts = append(lefts, left)
	}
	sort.Strings(lefts)
	pairs := make([]Pair, 0, len(lefts))
	for _, left := range lefts {
		pairs = append(pairs, Pair{Left: left, Right: r.leftToRight[left], Revision: r.revisions[left]})
	}
	return Snapshot{Generation: r.generation, NextRevision: r.nextRevision, Pairs: pairs}
}
