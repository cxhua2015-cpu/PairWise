package metacatalog296

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
// State is kept in a name-indexed map guarded by a single RWMutex.
// Apply executes each batch as an atomic candidate transaction against a
// scratch copy of the index and commits only on success.
type Store struct {
	mu         sync.RWMutex
	opts       Options
	records    map[string]Record
	generation uint64
	revision   uint64
	totalValue int
}

func New(o Options) (*Store, error) {
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
}

// Apply executes the batch atomically in input order. Put allocates a
// consecutive revision per op; Delete allocates none. Structural validation
// completes before any state is read, and record-count / total-value
// capacity is only checked against the final state. Any failure rolls back
// records, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: mutate a scratch index so a failure anywhere
	// leaves the committed state untouched.
	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	revision := s.revision
	totalValue := s.totalValue

	type outcome struct {
		name    string
		deleted bool
	}
	final := make(map[string]outcome, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			if old, ok := candidate[op.Name]; ok {
				totalValue -= len(old.Value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: revision}
			totalValue += len(value)
			final[op.Name] = outcome{name: op.Name}
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			totalValue -= len(old.Value)
			final[op.Name] = outcome{name: op.Name, deleted: true}
		}
	}

	// Capacity is enforced only against the final candidate state.
	if len(candidate) > s.opts.MaxRecords || totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.revision = revision
	s.totalValue = totalValue
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(final))
	for _, o := range final {
		if o.deleted {
			changed = append(changed, Record{Name: o.name})
		} else {
			rec := candidate[o.name]
			changed = append(changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: revision, Changed: changed}, nil
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
	rec.Value = append([]byte(nil), rec.Value...)
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
