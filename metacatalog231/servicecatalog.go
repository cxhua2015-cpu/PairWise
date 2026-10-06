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
// The zero value is not usable; construct it with New.
type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Put allocates
// consecutive revisions; Delete allocates none. On any failure all state,
// generation and revision are rolled back.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.validateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		next[name] = rec
	}

	changed := make(map[string]Record)
	revision := s.nextRevision
	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			rec := Record{Name: op.Name, Value: value, Revision: revision}
			next[op.Name] = rec
			changed[op.Name] = rec
			lastRevision = revision
			revision++
		case Delete:
			if _, ok := next[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
			changed[op.Name] = Record{Name: op.Name}
		}
	}

	total := 0
	for _, rec := range next {
		total += len(rec.Value)
	}
	if len(next) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = next
	s.generation++
	s.nextRevision = revision

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: lastRevision, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := changed[name]
		rec.Value = cloneBytes(rec.Value)
		out.Changed = append(out.Changed, rec)
	}
	return out, nil
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
	rec.Value = cloneBytes(rec.Value)
	return rec, true, nil
}

// Snapshot returns a deep copy of the whole catalog, records sorted by name.
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
		rec.Value = cloneBytes(rec.Value)
		snap.Records = append(snap.Records, rec)
	}
	return snap
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
