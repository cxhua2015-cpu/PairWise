package metacatalog231

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
// records maps name -> record and acts as the primary index.
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
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. The batch is first
// validated structurally as a whole, then applied to a candidate state;
// record-count and total-value-bytes capacity are checked only at the end.
// Any failure rolls back all state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	nextRevision := s.nextRevision
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := append([]byte(nil), op.Value...)
			rec := Record{Name: op.Name, Value: value, Revision: nextRevision}
			nextRevision++
			candidate[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			delete(changed, op.Name)
		}
	}

	total := 0
	for _, rec := range candidate {
		total += len(rec.Value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	res := Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := changed[name]
		res.Changed = append(res.Changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	return res, nil
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
	return Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := s.records[name]
		snap.Records = append(snap.Records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	return snap
}
