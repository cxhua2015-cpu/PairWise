package metacatalog376

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
	mu       sync.RWMutex
	opts     Options
	records  map[string]Record
	totalVal int
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before any state is read.
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

	// Candidate transaction: clone records so failure rolls back everything.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalVal
	nextRev := s.nextRev
	touched := map[string]bool{}

	for _, op := range b.Ops {
		touched[op.Name] = true
		if op.Kind == Put {
			if old, ok := cand[op.Name]; ok {
				total -= len(old.Value)
			}
			val := make([]byte, len(op.Value))
			copy(val, op.Value)
			cand[op.Name] = Record{Name: op.Name, Value: val, Revision: nextRev}
			total += len(val)
			nextRev++
		} else {
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
		}
	}

	// Capacity limits are only checked at the end of the batch.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := cand[name]; ok {
			val := make([]byte, len(r.Value))
			copy(val, r.Value)
			r.Value = val
			changed = append(changed, r)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	s.records = cand
	s.totalVal = total
	s.nextRev = nextRev
	if len(b.Ops) > 0 {
		s.gen++
	}
	return Result{Generation: s.gen, Revision: nextRev - 1, Changed: changed}, nil
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
	val := make([]byte, len(r.Value))
	copy(val, r.Value)
	r.Value = val
	return r, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		val := make([]byte, len(r.Value))
		copy(val, r.Value)
		r.Value = val
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.gen, NextRevision: s.nextRev, Records: recs}
}
