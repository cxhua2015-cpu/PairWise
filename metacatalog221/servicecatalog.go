package metacatalog221

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
type Store struct {
	mu              sync.RWMutex
	opts            Options
	records         map[string]*Record
	generation      uint64
	nextRevision    uint64
	totalValueBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]*Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Puts allocate
// consecutive revisions; Deletes allocate none. Capacity limits are
// checked only against the final state. Any failure rolls back all
// state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := validateBatch(s.opts, b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]*Record, len(s.records))
	total := s.totalValueBytes
	for name, r := range s.records {
		candidate[name] = r
	}
	nextRev := s.nextRevision
	touched := map[string]bool{}

	for _, op := range b.Ops {
		touched[op.Name] = true
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = &Record{Name: op.Name, Value: v, Revision: nextRev}
			total += len(v)
			nextRev++
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = candidate
	s.nextRevision = nextRev
	s.totalValueBytes = total

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := candidate[name]; ok {
			v := make([]byte, len(r.Value))
			copy(v, r.Value)
			changed = append(changed, Record{Name: r.Name, Value: v, Revision: r.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(s.opts, name) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return Record{Name: r.Name, Value: v, Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	records := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		records = append(records, Record{Name: r.Name, Value: v, Revision: r.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
