package metacatalog386

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
	totalBytes int
	generation uint64
	revision   uint64
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

func (s *Store) validate(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Full structural validation before any state is read.
	for _, op := range b.Ops {
		if err := s.validate(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Candidate transaction: apply ops in input order on a clone.
	cand := make(map[string]entry, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalBytes
	revision := s.revision
	touched := make(map[string]bool, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			if old, ok := cand[op.Name]; ok {
				total -= len(old.value)
			}
			cand[op.Name] = entry{value: v, revision: revision}
			total += len(v)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(cand, op.Name)
		}
		touched[op.Name] = true
	}

	// Capacity limits are checked only at the end of the batch.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalBytes = total
	s.revision = revision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if e, ok := cand[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
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
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		recs = append(recs, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
