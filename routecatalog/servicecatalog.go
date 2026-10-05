package routecatalog

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
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
	totalValue   int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

func validName(n string, max int) bool {
	if n == "" || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c > 127 || !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
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
		if op.Kind == Delete && len(op.Value) != 0 {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	cand := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	rev := s.nextRevision
	touched := make(map[string]bool)
	for _, op := range b.Ops {
		touched[op.Name] = true
		if op.Kind == Put {
			if old, ok := cand[op.Name]; ok {
				total -= len(old.Value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = Record{Name: op.Name, Value: v, Revision: rev}
			total += len(v)
			rev++
		} else {
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(cand, op.Name)
		}
	}
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.totalValue = total
	s.nextRevision = rev
	s.generation++

	names := make([]string, 0, len(touched))
	for n := range touched {
		if _, ok := cand[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, n := range names {
		r := cand[n]
		changed = append(changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return Result{Generation: s.generation, Revision: rev - 1, Changed: changed}, nil
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
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		r := s.records[n]
		recs = append(recs, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
