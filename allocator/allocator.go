// Package allocator provides a concurrency-safe interval allocator over a
// half-open address space [0, Size).
package allocator

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
	ErrNoSpace        = errors.New("no space")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	Size                         uint64
	MaxAllocations, MaxNameBytes int
}

type OpKind uint8

const (
	Reserve OpKind = iota + 1
	Allocate
	Free
)

type Op struct {
	Kind                     OpKind
	Name                     string
	Start, Length, Alignment uint64
}

type Batch struct{ Ops []Op }

type Allocation struct {
	Name                    string
	Start, Length, Revision uint64
}

type Result struct {
	Generation, Revision uint64
	Changed              []Allocation
}

type Snapshot struct {
	Generation, NextRevision, Size uint64
	Allocations                    []Allocation
}

type Allocator struct {
	mu        sync.Mutex
	size      uint64
	maxAllocs int
	maxName   int
	byName    map[string]Allocation
	sorted    []Allocation // sorted by Start, then Name
	nextRev   uint64
	gen       uint64
}

func New(o Options) (*Allocator, error) {
	if o.Size == 0 || o.MaxAllocations <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Allocator{
		size:      o.Size,
		maxAllocs: o.MaxAllocations,
		maxName:   o.MaxNameBytes,
		byName:    make(map[string]Allocation),
		nextRev:   1,
	}, nil
}

func validName(n string, max int) bool {
	if n == "" || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func isPow2(v uint64) bool { return v > 0 && v&(v-1) == 0 }

// validateOp performs structural validation only; it never reads state.
func (a *Allocator) validateOp(op Op) error {
	switch op.Kind {
	case Reserve:
		if !validName(op.Name, a.maxName) || op.Length == 0 || op.Alignment != 0 {
			return ErrInvalidInput
		}
		if op.Start > a.size || op.Length > a.size-op.Start {
			return ErrInvalidInput
		}
	case Allocate:
		if !validName(op.Name, a.maxName) || op.Start != 0 || op.Length == 0 || !isPow2(op.Alignment) {
			return ErrInvalidInput
		}
		if op.Length > a.size {
			return ErrInvalidInput
		}
	case Free:
		if !validName(op.Name, a.maxName) || op.Start != 0 || op.Length != 0 || op.Alignment != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// findLoc returns the lowest alignment-aligned start whose
// [start, start+length) range is free within sorted intervals.
// All arithmetic is overflow-safe; size and length are assumed nonzero
// with length <= size.
func findLoc(sorted []Allocation, size, length, alignment uint64) (uint64, bool) {
	gapStart := uint64(0)
	check := func(gapEnd uint64) (uint64, bool) {
		// first aligned >= gapStart, overflow-safe
		rem := gapStart % alignment
		var s uint64
		if rem == 0 {
			s = gapStart
		} else {
			delta := alignment - rem
			if delta > ^uint64(0)-gapStart {
				return 0, false
			}
			s = gapStart + delta
		}
		if s >= gapEnd || length > gapEnd-s {
			return 0, false
		}
		return s, true
	}
	for _, iv := range sorted {
		if gapStart < iv.Start {
			if s, ok := check(iv.Start); ok {
				return s, true
			}
		}
		if iv.Start+iv.Length > gapStart {
			gapStart = iv.Start + iv.Length
		}
	}
	if gapStart < size {
		if s, ok := check(size); ok {
			return s, true
		}
	}
	return 0, false
}

func overlaps(sorted []Allocation, start, length uint64) bool {
	end := start + length
	for _, iv := range sorted {
		if iv.Start >= end {
			break
		}
		if iv.Start+iv.Length > start {
			return true
		}
	}
	return false
}

func insertSorted(s []Allocation, al Allocation) []Allocation {
	i := sort.Search(len(s), func(i int) bool {
		if s[i].Start != al.Start {
			return s[i].Start > al.Start
		}
		return s[i].Name > al.Name
	})
	s = append(s, Allocation{})
	copy(s[i+1:], s[i:])
	s[i] = al
	return s
}

func removeSorted(s []Allocation, al Allocation) []Allocation {
	i := sort.Search(len(s), func(i int) bool {
		if s[i].Start != al.Start {
			return s[i].Start >= al.Start
		}
		return s[i].Name >= al.Name
	})
	if i < len(s) && s[i].Name == al.Name && s[i].Start == al.Start {
		return append(s[:i], s[i+1:]...)
	}
	return s
}

func (a *Allocator) Apply(b Batch) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Phase 1: structural validation of every op, in input order,
	// without reading state.
	for _, op := range b.Ops {
		if err := a.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	// Phase 2: execute on an isolated candidate.
	byName := make(map[string]Allocation, len(a.byName)+len(b.Ops))
	for k, v := range a.byName {
		byName[k] = v
	}
	sorted := make([]Allocation, len(a.sorted))
	copy(sorted, a.sorted)
	nextRev := a.nextRev
	touched := make(map[string]Allocation, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Reserve:
			if _, dup := byName[op.Name]; dup {
				return Result{}, ErrConflict
			}
			if overlaps(sorted, op.Start, op.Length) {
				return Result{}, ErrConflict
			}
			al := Allocation{Name: op.Name, Start: op.Start, Length: op.Length, Revision: nextRev}
			nextRev++
			byName[op.Name] = al
			sorted = insertSorted(sorted, al)
			touched[op.Name] = al
		case Allocate:
			if _, dup := byName[op.Name]; dup {
				return Result{}, ErrConflict
			}
			start, ok := findLoc(sorted, a.size, op.Length, op.Alignment)
			if !ok {
				return Result{}, ErrNoSpace
			}
			al := Allocation{Name: op.Name, Start: start, Length: op.Length, Revision: nextRev}
			nextRev++
			byName[op.Name] = al
			sorted = insertSorted(sorted, al)
			touched[op.Name] = al
		case Free:
			al, ok := byName[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(byName, op.Name)
			sorted = removeSorted(sorted, al)
			delete(touched, op.Name)
		}
	}

	if len(byName) > a.maxAllocs {
		return Result{}, ErrCapacity
	}

	// Commit.
	a.byName = byName
	a.sorted = sorted
	a.nextRev = nextRev
	if len(b.Ops) > 0 {
		a.gen++
	}

	changed := make([]Allocation, 0, len(touched))
	for _, al := range touched {
		changed = append(changed, al)
	}
	sortAllocations(changed)
	return Result{Generation: a.gen, Revision: nextRev - 1, Changed: changed}, nil
}

func sortAllocations(s []Allocation) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Start != s[j].Start {
			return s[i].Start < s[j].Start
		}
		return s[i].Name < s[j].Name
	})
}

func (a *Allocator) Find(length, alignment uint64) (uint64, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if length == 0 || !isPow2(alignment) || length > a.size {
		return 0, false, ErrInvalidInput
	}
	start, ok := findLoc(a.sorted, a.size, length, alignment)
	return start, ok, nil
}

func (a *Allocator) Lookup(name string) (Allocation, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !validName(name, a.maxName) {
		return Allocation{}, false, ErrInvalidInput
	}
	al, ok := a.byName[name]
	return al, ok, nil
}

func (a *Allocator) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	allocs := make([]Allocation, len(a.sorted))
	copy(allocs, a.sorted)
	return Snapshot{
		Generation:   a.gen,
		NextRevision: a.nextRev,
		Size:         a.size,
		Allocations:  allocs,
	}
}
