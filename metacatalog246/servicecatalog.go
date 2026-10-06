package metacatalog246

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
// records maps name -> record; revision is the last assigned revision
// (NextRevision - 1); generation counts successful non-empty batches.
type Store struct {
	mu         sync.RWMutex
	opts       Options
	records    map[string]Record
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
}

// Apply executes a batch atomically in input order. On any failure all
// state, generation and revision are rolled back.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: mutate a private copy, commit only on success.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	revision := s.revision
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			rec := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: revision}
			cand[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(changed, op.Name)
		}
	}

	// Capacity limits are checked only at the end of the batch.
	total := 0
	for _, rec := range cand {
		total += len(rec.Value)
	}
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}
	out := make([]Record, 0, len(changed))
	for _, rec := range changed {
		out = append(out, Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Result{Generation: s.generation, Revision: s.revision, Changed: out}, nil
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
	return Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
