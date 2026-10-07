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

// Apply executes the batch atomically in input order. Puts allocate
// consecutive revisions; Deletes do not. Any failure rolls back all state,
// generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	work := make(map[string]Record, len(s.records)+len(b.Ops))
	for name, rec := range s.records {
		work[name] = rec
	}

	rev := s.nextRevision
	final := make(map[string]*Record, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rec := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: rev}
			rev++
			work[op.Name] = rec
			r := rec
			final[op.Name] = &r
		case Delete:
			if _, ok := work[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(work, op.Name)
			final[op.Name] = nil
		}
	}

	if len(work) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, rec := range work {
		total += len(rec.Value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = work
	s.nextRevision = rev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(final))
	for name, rec := range final {
		if rec != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		changed = append(changed, *final[name])
	}

	return Result{Generation: s.generation, Revision: rev - 1, Changed: changed}, nil
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
	rec.Value = cloneBytes(rec.Value)
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, name := range names {
		rec := s.records[name]
		rec.Value = cloneBytes(rec.Value)
		recs = append(recs, rec)
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
