package resourcecatalog191

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

type record struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu         sync.Mutex
	opts       Options
	records    map[string]record
	generation uint64
	revision   uint64
	totalValue int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]record)}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before any state read.
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

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Candidate transaction: mutate copies, commit only on success.
	cand := make(map[string]record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	revision := s.revision
	total := s.totalValue
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Name]; ok {
				total -= len(old.value)
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			cand[op.Name] = record{value: v, revision: revision}
			total += len(v)
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(cand, op.Name)
		}
	}

	// Capacity limits checked only at batch end.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.revision = revision
	s.totalValue = total
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := cand[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	if len(s.records) > 0 {
		snap.Records = make([]Record, 0, len(s.records))
		for name, r := range s.records {
			snap.Records = append(snap.Records, Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision})
		}
		sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	}
	return snap
}
