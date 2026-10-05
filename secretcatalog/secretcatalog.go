package secretcatalog

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

type entry struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu       sync.RWMutex
	records  map[string]entry
	total    int
	gen      uint64
	nextRev  uint64
	maxRec   int
	maxName  int
	maxVal   int
	maxTotal int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		records:  make(map[string]entry),
		nextRev:  1,
		maxRec:   o.MaxRecords,
		maxName:  o.MaxNameBytes,
		maxVal:   o.MaxValueBytes,
		maxTotal: o.MaxTotalValueBytes,
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.maxName) {
			return Result{}, ErrInvalidInput
		}
		if len(op.Value) > s.maxVal {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	// Phase 2: candidate transaction on a cloned index.
	cand := make(map[string]entry, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.total
	nextRev := s.nextRev
	final := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = entry{value: v, revision: nextRev}
			total += len(v)
			final[op.Name] = Record{Name: op.Name, Value: v, Revision: nextRev}
			nextRev++
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(cand, op.Name)
			final[op.Name] = Record{Name: op.Name}
		}
	}

	// Phase 3: capacity checked only at batch end.
	if len(cand) > s.maxRec || total > s.maxTotal {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.total = total
	s.nextRev = nextRev
	s.gen++

	changed := make([]Record, 0, len(final))
	for _, r := range final {
		if r.Value != nil {
			v := make([]byte, len(r.Value))
			copy(v, r.Value)
			r.Value = v
		}
		changed = append(changed, r)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.gen, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.maxName) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: name, Value: v, Revision: e.revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.gen, NextRevision: s.nextRev, Records: recs}
}
