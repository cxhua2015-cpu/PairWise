package resourcecatalog111

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
	// Full structural validation before touching any state.
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

	// Candidate transaction: apply ops in input order on a clone.
	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalBytes
	revision := s.revision
	changed := make(map[string]Record, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			old, existed := candidate[op.Name]
			if existed {
				total -= len(old.Value)
			}
			val := make([]byte, len(op.Value))
			copy(val, op.Value)
			rec := Record{Name: op.Name, Value: val, Revision: revision}
			candidate[op.Name] = rec
			changed[op.Name] = rec
			total += len(val)
		case Delete:
			old, existed := candidate[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			delete(changed, op.Name)
			total -= len(old.Value)
		}
	}

	// Capacity limits are enforced only at the end of the batch.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalBytes = total
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	res := Result{Generation: s.generation, Revision: s.revision}
	if len(changed) > 0 {
		names := make([]string, 0, len(changed))
		for name := range changed {
			names = append(names, name)
		}
		sort.Strings(names)
		res.Changed = make([]Record, 0, len(names))
		for _, name := range names {
			rec := changed[name]
			val := make([]byte, len(rec.Value))
			copy(val, rec.Value)
			res.Changed = append(res.Changed, Record{Name: rec.Name, Value: val, Revision: rec.Revision})
		}
	}
	return res, nil
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
	val := make([]byte, len(rec.Value))
	copy(val, rec.Value)
	return Record{Name: rec.Name, Value: val, Revision: rec.Revision}, true, nil
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
		val := make([]byte, len(rec.Value))
		copy(val, rec.Value)
		recs = append(recs, Record{Name: rec.Name, Value: val, Revision: rec.Revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
