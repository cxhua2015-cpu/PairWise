package featureflags

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
	records    map[string]entry
	totalValue int
	generation uint64
	revision   uint64
	opts       Options
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{records: make(map[string]entry), opts: o}, nil
}

func validName(n string, max int) bool {
	if len(n) == 0 || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before any state read.
	for _, op := range b.Ops {
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > s.opts.MaxValueBytes {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if len(op.Value) != 0 {
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

	// Candidate transaction: apply ops on a clone, commit only on success.
	cand := make(map[string]entry, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	revision := s.revision
	changed := make(map[string]Record)

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
			changed[op.Name] = Record{Name: op.Name, Value: v, Revision: revision}
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(cand, op.Name)
			delete(changed, op.Name)
		}
	}

	// Capacity checked only at batch end.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalValue = total
	s.revision = revision
	s.generation++

	res := Result{Generation: s.generation, Revision: s.revision}
	if len(changed) > 0 {
		names := make([]string, 0, len(changed))
		for n := range changed {
			names = append(names, n)
		}
		sort.Strings(names)
		res.Changed = make([]Record, 0, len(names))
		for _, n := range names {
			r := changed[n]
			res.Changed = append(res.Changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
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
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision}, true, nil
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
		snap.Records = append(snap.Records, Record{Name: n, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	return snap
}
