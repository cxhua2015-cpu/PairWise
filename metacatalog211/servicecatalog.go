package metacatalog211

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
	mu         sync.Mutex
	opts       Options
	records    map[string]entry
	generation uint64
	nextRev    uint64
	totalBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry), nextRev: 1}, nil
}

func validName(n string, max int) bool {
	if len(n) == 0 || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 2: candidate transaction on a cloned index.
	cand := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	nextRev := s.nextRev
	total := s.totalBytes
	touched := map[string]bool{}
	for _, op := range b.Ops {
		touched[op.Name] = true
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = entry{value: v, revision: nextRev}
			nextRev++
			total += len(v)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			total -= len(old.value)
		}
	}

	// Phase 3: final capacity checks at batch end.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.nextRev = nextRev
	s.totalBytes = total
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(touched))
	for n := range touched {
		names = append(names, n)
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, n := range names {
		if e, ok := cand[n]; ok {
			v := make([]byte, len(e.value))
			copy(v, e.value)
			changed = append(changed, Record{Name: n, Value: v, Revision: e.revision})
		} else {
			changed = append(changed, Record{Name: n})
		}
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		e := s.records[n]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: n, Value: v, Revision: e.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRev, Records: recs}
}
