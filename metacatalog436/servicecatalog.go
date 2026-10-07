package metacatalog436

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
//
// records maps name to its current record. Record.Value slices stored here
// are never mutated after being installed, so shallow map copies may share
// them safely; all values crossing the API boundary are deep-copied.
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
	return &Store{
		opts:         opts,
		records:      make(map[string]Record),
		nextRevision: 1,
	}, nil
}

// applyInner executes the batch atomically against the given state and
// returns the result plus the new state. On error the returned state is the
// untouched input, so callers can simply discard it to roll back.
func applyInner(opts Options, records map[string]Record, generation, nextRevision uint64, b Batch) (Result, map[string]Record, uint64, uint64, error) {
	if err := validateBatch(opts, b); err != nil {
		return Result{}, records, generation, nextRevision, err
	}
	if len(b.Ops) == 0 {
		return Result{}, records, generation, nextRevision, nil
	}

	next := make(map[string]Record, len(records)+len(b.Ops))
	for name, rec := range records {
		next[name] = rec
	}
	touched := make(map[string]struct{}, len(b.Ops))
	revision := nextRevision
	var lastRevision uint64

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			next[op.Name] = Record{Name: op.Name, Value: value, Revision: revision}
			lastRevision = revision
			revision++
			touched[op.Name] = struct{}{}
		case Delete:
			if _, ok := next[op.Name]; !ok {
				return Result{}, records, generation, nextRevision, ErrNotFound
			}
			delete(next, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	if len(next) > opts.MaxRecords {
		return Result{}, records, generation, nextRevision, ErrCapacity
	}
	total := 0
	for _, rec := range next {
		total += len(rec.Value)
		if total > opts.MaxTotalValueBytes {
			return Result{}, records, generation, nextRevision, ErrCapacity
		}
	}

	generation++
	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := next[name]; ok {
			changed = append(changed, cloneRecord(rec))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: generation, Revision: lastRevision, Changed: changed}, next, generation, revision, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, records, generation, nextRevision, err := applyInner(s.opts, s.records, s.generation, s.nextRevision, b)
	if err != nil {
		return Result{}, err
	}
	s.records = records
	s.generation = generation
	s.nextRevision = nextRevision
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(s.opts, name); err != nil {
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
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, cloneRecord(rec))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}

func cloneRecord(rec Record) Record {
	value := make([]byte, len(rec.Value))
	copy(value, rec.Value)
	return Record{Name: rec.Name, Value: value, Revision: rec.Revision}
}
