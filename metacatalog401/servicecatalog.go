package metacatalog401

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
// records is the committed index; candidate transactions clone it,
// mutate the clone, and swap it in only after all end-of-batch checks pass.
type Store struct {
	mu              sync.RWMutex
	opts            Options
	records         map[string]Record
	generation      uint64
	nextRevision    uint64
	totalValueBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Puts allocate
// consecutive revisions; Deletes allocate none. Structural validation runs
// before any state read; record-count and total-value-bytes capacity are
// checked only at the end of the batch. Any failure rolls back all state,
// generation, and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: work on a private copy of the index.
	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValueBytes
	nextRev := s.nextRevision
	touched := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: nextRev}
			nextRev++
			candidate[op.Name] = rec
			total += len(rec.Value)
			touched[op.Name] = rec
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			delete(touched, op.Name)
		}
	}

	// End-of-batch capacity checks.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalValueBytes = total
	s.nextRevision = nextRev
	s.generation++

	changed := make([]Record, 0, len(touched))
	for _, rec := range touched {
		changed = append(changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

// Get returns a deep copy of the named record.
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
	return Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}, true, nil
}

// Snapshot returns a deep copy of all records sorted by name.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
