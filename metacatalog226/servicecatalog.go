package metacatalog226

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
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record), nextRevision: 1}, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func cloneRecord(r Record) Record {
	r.Value = cloneBytes(r.Value)
	return r
}

// Apply executes the batch atomically in input order. Structural validation
// happens before any state is read; capacity limits are checked only at the
// end of the batch. Any failure rolls back records, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: buffer mutations without touching live state.
	type mutation struct {
		rec    Record
		exists bool // record visible at this point in the batch
	}
	pending := make(map[string]*mutation)
	order := make([]string, 0, len(b.Ops))
	stage := func(name string) *mutation {
		m, ok := pending[name]
		if !ok {
			m = &mutation{}
			if cur, exists := s.records[name]; exists {
				m.rec = cur
				m.exists = true
			}
			pending[name] = m
			order = append(order, name)
		}
		return m
	}

	nextRev := s.nextRevision
	for _, op := range b.Ops {
		m := stage(op.Name)
		switch op.Kind {
		case Put:
			nextRev++
			m.rec = Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: nextRev - 1}
			m.exists = true
		case Delete:
			if !m.exists {
				return Result{}, ErrNotFound
			}
			m.exists = false
		}
	}

	// End-of-batch capacity checks against the candidate final state.
	finalRecords := len(s.records)
	finalTotal := s.totalValue
	for _, name := range order {
		m := pending[name]
		cur, exists := s.records[name]
		switch {
		case !m.exists:
			if exists {
				finalRecords--
				finalTotal -= len(cur.Value)
			}
		default:
			if exists {
				finalTotal += len(m.rec.Value) - len(cur.Value)
			} else {
				finalRecords++
				finalTotal += len(m.rec.Value)
			}
		}
	}
	if finalRecords > s.opts.MaxRecords || finalTotal > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	for _, name := range order {
		m := pending[name]
		if !m.exists {
			if cur, exists := s.records[name]; exists {
				s.totalValue -= len(cur.Value)
				delete(s.records, name)
			}
		} else {
			if cur, exists := s.records[name]; exists {
				s.totalValue += len(m.rec.Value) - len(cur.Value)
			} else {
				s.totalValue += len(m.rec.Value)
			}
			s.records[name] = m.rec
		}
	}
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(order))
	for _, name := range order {
		if m := pending[name]; m.exists {
			changed = append(changed, cloneRecord(m.rec))
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(r), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, cloneRecord(r))
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
