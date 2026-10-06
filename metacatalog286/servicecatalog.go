package metacatalog286

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
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Structural validation
// happens before any state read; capacity limits are checked only against the
// final candidate state. Any failure rolls back all state and clocks.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	net := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := candidate[op.Name]
			if existed {
				total -= len(old.Value)
			}
			value := append([]byte(nil), op.Value...)
			rec := Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = rec
			total += len(value)
			net[op.Name] = rec
		case Delete:
			old, existed := candidate[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			net[op.Name] = Record{Name: op.Name}
		}
	}

	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(net))
	for _, rec := range net {
		changed = append(changed, cloneRecord(rec))
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
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

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, cloneRecord(rec))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
