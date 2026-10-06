package metacatalog236

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
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	next := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		next[k] = v
	}
	changed := make(map[string]Record, len(b.Ops))
	revision := s.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			r := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: revision}
			revision++
			next[op.Name] = r
			changed[op.Name] = r
		case Delete:
			if _, ok := next[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
			delete(changed, op.Name)
		}
	}

	total := 0
	for _, r := range next {
		total += len(r.Value)
	}
	if len(next) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = next
	s.nextRevision = revision
	s.generation++

	out := make([]Record, 0, len(changed))
	for _, r := range changed {
		out = append(out, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Result{Generation: s.generation, Revision: revision - 1, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.nameLimit()) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	r.Value = cloneBytes(r.Value)
	return r, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}

func (s *Store) nameLimit() int { return s.opts.MaxNameBytes }
