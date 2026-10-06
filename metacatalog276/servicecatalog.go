package metacatalog276

import (
	"errors"
	"sort"
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
	totalBytes   int
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:         opts,
		records:      make(map[string]Record),
		nextRevision: 1,
	}, nil
}

// Apply executes a batch atomically in input order. Structural validation
// happens before any state is read; record-count and total-value-byte
// capacities are checked only against the final state. Any failure rolls
// back records, generation and revision.
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
	// rollback by simply discarding it.
	candidate := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	totalBytes := s.totalBytes
	nextRevision := s.nextRevision
	final := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := append([]byte(nil), op.Value...)
			if old, ok := candidate[op.Name]; ok {
				totalBytes -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: value, Revision: nextRevision}
			nextRevision++
			candidate[op.Name] = rec
			totalBytes += len(value)
			final[op.Name] = rec
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			totalBytes -= len(old.Value)
			final[op.Name] = Record{Name: op.Name}
		}
	}

	if len(candidate) > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalBytes = totalBytes
	s.nextRevision = nextRevision
	s.generation++

	changed := make([]Record, 0, len(final))
	for _, rec := range final {
		changed = append(changed, cloneRecord(rec))
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

// Get returns a deep copy of the named record.
func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.opts.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

// Snapshot returns a name-sorted deep copy of all records plus logical clocks.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	snap := Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Records:      make([]Record, 0, len(s.records)),
	}
	for _, rec := range s.records {
		snap.Records = append(snap.Records, cloneRecord(rec))
	}
	sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	return snap
}

func cloneRecord(rec Record) Record {
	rec.Value = append([]byte(nil), rec.Value...)
	return rec
}
