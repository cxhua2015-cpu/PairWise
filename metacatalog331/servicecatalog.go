package metacatalog331

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

type Store struct {
	mu         sync.RWMutex
	opt        Options
	records    map[string]Record
	totalValue int
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opt: o, records: make(map[string]Record)}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Full structural validation before any state is read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opt.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opt.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Candidate transaction: clone state, apply ops in input order.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	oldRevision := s.revision
	revision := s.revision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.Value)
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
			total += len(v)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
		}
	}

	// Capacity limits are checked only at the end of the batch.
	if len(cand) > s.opt.MaxRecords || total > s.opt.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalValue = total
	s.revision = revision
	s.generation++

	// Changed holds the final record of every name whose last write in this
	// batch was a Put (i.e. its revision was allocated by this batch).
	changed := make([]Record, 0, len(b.Ops))
	for _, r := range cand {
		if r.Revision > oldRevision {
			changed = append(changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opt.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
