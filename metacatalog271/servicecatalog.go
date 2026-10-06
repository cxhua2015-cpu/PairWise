package metacatalog271

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

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// Apply executes the batch atomically in input order. Put allocates a
// consecutive revision per op; Delete allocates none. Structural validation
// happens before any state read; record-count and total-value capacity are
// checked only at the end of the batch. Any failure rolls back all state,
// generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: apply ops on a shallow copy of the index.
	// Records are immutable values, so sharing []byte with the committed
	// state is safe; Puts store freshly copied values.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := cand[op.Name]
			if existed {
				total -= len(old.Value)
			}
			v := cloneBytes(op.Value)
			rec := Record{Name: op.Name, Value: v, Revision: nextRev}
			nextRev++
			cand[op.Name] = rec
			changed[op.Name] = rec
			total += len(v)
		case Delete:
			old, existed := cand[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(changed, op.Name)
			total -= len(old.Value)
		}
	}
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = cand
	s.totalValue = total
	s.nextRevision = nextRev

	out := make([]Record, 0, len(changed))
	for _, r := range changed {
		out = append(out, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
