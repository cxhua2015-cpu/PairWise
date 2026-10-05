package endpointcatalog

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
	mu         sync.RWMutex
	opts       Options
	records    map[string]entry
	generation uint64
	revision   uint64
	totalBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry)}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching any state.
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

	// Phase 2: build candidate state by replaying ops in input order.
	cand := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	candTotal := s.totalBytes
	revision := s.revision
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			if old, ok := cand[op.Name]; ok {
				candTotal -= len(old.value)
			}
			cand[op.Name] = entry{value: v, revision: revision}
			candTotal += len(v)
			changed[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			candTotal -= len(old.value)
			delete(cand, op.Name)
			changed[op.Name] = Record{Name: op.Name}
		}
	}

	// Phase 3: capacity limits checked only against the final candidate state.
	if len(cand) > s.opts.MaxRecords || candTotal > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.totalBytes = candTotal
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: s.revision, Changed: make([]Record, 0, len(names))}
	for _, n := range names {
		r := changed[n]
		if r.Value != nil {
			v := make([]byte, len(r.Value))
			copy(v, r.Value)
			r.Value = v
		}
		out.Changed = append(out.Changed, r)
	}
	return out, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
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
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	snap := Snapshot{
		Generation:   s.generation,
		NextRevision: s.revision + 1,
		Records:      make([]Record, 0, len(names)),
	}
	for _, n := range names {
		e := s.records[n]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		snap.Records = append(snap.Records, Record{Name: n, Value: v, Revision: e.revision})
	}
	return snap
}
