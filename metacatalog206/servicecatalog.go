package metacatalog206

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
	totalBytes int
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
}

func validName(name string, max int) bool {
	if len(name) == 0 || len(name) > max {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
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

	// Phase 2: candidate transaction on a cloned index.
	cand := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalBytes
	revision := s.revision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := cand[op.Name]
			if existed {
				total -= len(old.Value)
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
			total += len(v)
			touched[op.Name] = struct{}{}
		case Delete:
			old, existed := cand[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	// Phase 3: capacity limits checked only at batch end.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.totalBytes = total
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := cand[name]; ok {
			v := make([]byte, len(r.Value))
			copy(v, r.Value)
			changed = append(changed, Record{Name: r.Name, Value: v, Revision: r.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return Record{Name: r.Name, Value: v, Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		recs = append(recs, Record{Name: r.Name, Value: v, Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
