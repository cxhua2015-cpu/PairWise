package resourcecatalog156

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
	opts       Options
	records    map[string]Record
	generation uint64
	revision   uint64
	totalValue int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
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

func cloneValue(v []byte) []byte {
	if v == nil {
		return nil
	}
	c := make([]byte, len(v))
	copy(c, v)
	return c
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) > s.opts.MaxValueBytes {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) != 0 {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Phase 2: candidate transaction on a cloned index.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	revision := s.revision
	changed := make(map[string]Record, len(b.Ops))
	deleted := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			old, existed := cand[op.Name]
			if existed {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: cloneValue(op.Value), Revision: revision}
			cand[op.Name] = rec
			total += len(rec.Value)
			changed[op.Name] = rec
			delete(deleted, op.Name)
		case Delete:
			old, existed := cand[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
			deleted[op.Name] = struct{}{}
		}
	}

	// Phase 3: capacity limits checked only at batch end.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.totalValue = total
	s.revision = revision
	s.generation++

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Record, 0, len(names))
	for _, name := range names {
		rec := changed[name]
		rec.Value = cloneValue(rec.Value)
		out = append(out, rec)
	}
	return Result{Generation: s.generation, Revision: s.revision, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	rec.Value = cloneValue(rec.Value)
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, name := range names {
		rec := s.records[name]
		rec.Value = cloneValue(rec.Value)
		recs = append(recs, rec)
	}
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
