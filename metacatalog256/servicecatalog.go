package metacatalog256

import (
	"errors"
	"slices"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxRecords, MaxNameBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind  Kind
	Name  string
	Value []byte
}
type Batch struct{ Ops []Op }
type Record struct {
	Name     string
	Value    []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Record
}
type Snapshot struct {
	Generation, NextRevision uint64
	Records                  []Record
}

// Store is a concurrency-safe in-memory metadata catalog.
// The zero value is not usable; construct it with New.
type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:         o,
		records:      make(map[string]Record),
		nextRevision: 1,
	}, nil
}

// Apply executes the batch atomically in input order. Puts allocate
// consecutive revisions; deletes allocate none. Record-count and total
// value-byte capacities are checked only at the end of the batch. Any
// failure rolls back all state, generation and revision changes.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	// Candidate transaction: mutate a private copy so any failure is a
	// no-op on the committed state.
	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	touched := make(map[string]struct{}, len(b.Ops))
	nextRevision := s.nextRevision

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			candidate[op.Name] = Record{
				Name:     op.Name,
				Value:    slices.Clone(op.Value),
				Revision: nextRevision,
			}
			nextRevision++
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		touched[op.Name] = struct{}{}
	}

	totalValueBytes := 0
	for _, rec := range candidate {
		totalValueBytes += len(rec.Value)
	}
	if len(candidate) > s.opts.MaxRecords || totalValueBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := candidate[name]; ok {
			changed = append(changed, Record{
				Name:     rec.Name,
				Value:    slices.Clone(rec.Value),
				Revision: rec.Revision,
			})
		}
	}
	slices.SortFunc(changed, func(a, b Record) int { return compareStrings(a.Name, b.Name) })

	s.records = candidate
	s.generation++
	s.nextRevision = nextRevision

	return Result{Generation: s.generation, Revision: nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	rec.Value = slices.Clone(rec.Value)
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Records:      make([]Record, 0, len(s.records)),
	}
	for _, rec := range s.records {
		snap.Records = append(snap.Records, Record{
			Name:     rec.Name,
			Value:    slices.Clone(rec.Value),
			Revision: rec.Revision,
		})
	}
	slices.SortFunc(snap.Records, func(a, b Record) int { return compareStrings(a.Name, b.Name) })
	return snap
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
