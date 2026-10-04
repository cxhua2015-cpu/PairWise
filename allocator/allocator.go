// Package allocator provides a concurrency-safe interval allocator over a
// half-open address space [0, Size). See SPEC.md for the full contract.
package allocator

import (
	"errors"
	"math"
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

// Allocator manages the half-open interval [0, Size). The zero value is not
// usable; construct with New. All methods are safe for concurrent use.
type Allocator struct {
	mu             sync.RWMutex
	size           uint64
	maxAllocations int
	maxNameBytes   int
	generation     uint64
	nextRevision   uint64 // next revision to assign; starts at 1
	byName         map[string]Allocation
	sorted         []Allocation // sorted by Start then Name; starts are unique
}

// New creates an Allocator. All options must be positive.
func New(o Options) (*Allocator, error) {
	if o.Size == 0 || o.MaxAllocations <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Allocator{
		size:           o.Size,
		maxAllocations: o.MaxAllocations,
		maxNameBytes:   o.MaxNameBytes,
		nextRevision:   1,
		byName:         make(map[string]Allocation),
	}, nil
}

// Apply validates the whole batch structurally, then executes it atomically
// against an isolated candidate state. Any error rolls back everything,
// including generation and revision allocation.
func (a *Allocator) Apply(b Batch) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, op := range b.Ops {
		if err := a.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: a.generation, Revision: a.revision()}, nil
	}

	// Candidate state: clone-on-write so a failure leaves no trace.
	byName := make(map[string]Allocation, len(a.byName))
	for k, v := range a.byName {
		byName[k] = v
	}
	sorted := append([]Allocation(nil), a.sorted...)
	nextRevision := a.nextRevision
	changed := make(map[string]Allocation)

	for _, op := range b.Ops {
		switch op.Kind {
		case Reserve, Allocate:
			if _, exists := byName[op.Name]; exists {
				return Result{}, ErrConflict
			}
			start := op.Start
			if op.Kind == Allocate {
				var ok bool
				start, ok = findFree(sorted, a.size, op.Length, op.Alignment)
				if !ok {
					return Result{}, ErrNoSpace
				}
			} else if overlaps(sorted, op.Start, op.Length) {
				return Result{}, ErrConflict
			}
			alloc := Allocation{Name: op.Name, Start: start, Length: op.Length, Revision: nextRevision}
			nextRevision++
			byName[op.Name] = alloc
			sorted = insertSorted(sorted, alloc)
			changed[op.Name] = alloc
		case Free:
			alloc, exists := byName[op.Name]
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(byName, op.Name)
			sorted = removeSorted(sorted, alloc)
			delete(changed, op.Name)
		}
	}

	if len(byName) > a.maxAllocations {
		return Result{}, ErrCapacity
	}

	a.byName = byName
	a.sorted = sorted
	a.nextRevision = nextRevision
	a.generation++

	changedList := make([]Allocation, 0, len(changed))
	for _, alloc := range changed {
		changedList = append(changedList, alloc)
	}
	sortAllocations(changedList)
	return Result{Generation: a.generation, Revision: nextRevision - 1, Changed: changedList}, nil
}

// Find returns the lowest aligned start whose full range is free, without
// modifying any state.
func (a *Allocator) Find(length, alignment uint64) (uint64, bool, error) {
	if length == 0 || alignment == 0 || alignment&(alignment-1) != 0 {
		return 0, false, ErrInvalidInput
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	start, ok := findFree(a.sorted, a.size, length, alignment)
	return start, ok, nil
}

// Lookup returns the allocation for a valid name.
func (a *Allocator) Lookup(name string) (Allocation, bool, error) {
	if !a.validName(name) {
		return Allocation{}, false, ErrInvalidInput
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	alloc, ok := a.byName[name]
	return alloc, ok, nil
}

// Snapshot returns a stable, ownership-isolated view sorted by Start then Name.
func (a *Allocator) Snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return Snapshot{
		Generation:   a.generation,
		NextRevision: a.nextRevision,
		Size:         a.size,
		Allocations:  append([]Allocation(nil), a.sorted...),
	}
}

func (a *Allocator) revision() uint64 {
	if a.nextRevision == 1 {
		return 0
	}
	return a.nextRevision - 1
}

func (a *Allocator) validName(name string) bool {
	if len(name) == 0 || len(name) > a.maxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

// validateOp performs structural validation only; it never reads state.
func (a *Allocator) validateOp(op Op) error {
	if !a.validName(op.Name) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Reserve:
		if op.Length == 0 || op.Alignment != 0 {
			return ErrInvalidInput
		}
		if op.Start > math.MaxUint64-op.Length {
			return ErrInvalidInput
		}
		if op.Start+op.Length > a.size {
			return ErrInvalidInput
		}
	case Allocate:
		if op.Start != 0 || op.Length == 0 || op.Alignment == 0 || op.Alignment&(op.Alignment-1) != 0 {
			return ErrInvalidInput
		}
	case Free:
		if op.Start != 0 || op.Length != 0 || op.Alignment != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func sortAllocations(s []Allocation) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Start != s[j].Start {
			return s[i].Start < s[j].Start
		}
		return s[i].Name < s[j].Name
	})
}

// searchIdx returns the insertion point for start in the sorted slice.
func searchIdx(s []Allocation, start uint64) int {
	return sort.Search(len(s), func(i int) bool { return s[i].Start >= start })
}

func insertSorted(s []Allocation, alloc Allocation) []Allocation {
	i := searchIdx(s, alloc.Start)
	s = append(s, Allocation{})
	copy(s[i+1:], s[i:])
	s[i] = alloc
	return s
}

func removeSorted(s []Allocation, alloc Allocation) []Allocation {
	i := searchIdx(s, alloc.Start)
	if i < len(s) && s[i].Start == alloc.Start && s[i].Name == alloc.Name {
		copy(s[i:], s[i+1:])
		return s[:len(s)-1]
	}
	return s
}

// overlaps reports whether [start, start+length) intersects any allocation.
// Callers must ensure start+length does not overflow.
func overlaps(s []Allocation, start, length uint64) bool {
	end := start + length
	i := searchIdx(s, start)
	if i < len(s) && s[i].Start < end {
		return true
	}
	if i > 0 && s[i-1].Start+s[i-1].Length > start {
		return true
	}
	return false
}

// alignUp rounds v up to a multiple of the power-of-two a. The second result
// is false if the computation would overflow uint64.
func alignUp(v, a uint64) (uint64, bool) {
	if v > math.MaxUint64-(a-1) {
		return 0, false
	}
	return (v + a - 1) &^ (a - 1), true
}

// findFree scans the gaps between sorted allocations and returns the lowest
// aligned start whose full range [start, start+length) is free within size.
func findFree(sorted []Allocation, size, length, alignment uint64) (uint64, bool) {
	cursor := uint64(0)
	fit := func(gapEnd uint64) (uint64, bool) {
		start, ok := alignUp(cursor, alignment)
		if !ok || start > gapEnd || length > gapEnd-start {
			return 0, false
		}
		return start, true
	}
	for _, alloc := range sorted {
		if start, ok := fit(alloc.Start); ok {
			return start, true
		}
		cursor = alloc.Start + alloc.Length
	}
	return fit(size)
}
