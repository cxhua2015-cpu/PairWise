package metacatalog331

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
	mu           sync.Mutex
	opts         Options
	records      map[string]entry
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry), nextRevision: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

func (s *Store) validate(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	type backup struct {
		e      entry
		exists bool
	}
	touched := make(map[string]backup)
	changed := make(map[string]Record)
	var lastRev uint64
	savedNextRevision := s.nextRevision
	savedTotalValue := s.totalValue

	restore := func() {
		for name, bak := range touched {
			if bak.exists {
				s.records[name] = bak.e
			} else {
				delete(s.records, name)
			}
		}
		s.nextRevision = savedNextRevision
		s.totalValue = savedTotalValue
	}

	keep := func(name string) {
		if _, ok := touched[name]; !ok {
			e, exists := s.records[name]
			touched[name] = backup{e: e, exists: exists}
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			keep(op.Name)
			rev := s.nextRevision
			s.nextRevision++
			lastRev = rev
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			old, existed := s.records[op.Name]
			if existed {
				s.totalValue -= len(old.value)
			}
			s.records[op.Name] = entry{value: v, revision: rev}
			s.totalValue += len(v)
			changed[op.Name] = Record{Name: op.Name, Value: v, Revision: rev}
		case Delete:
			e, ok := s.records[op.Name]
			if !ok {
				restore()
				return Result{}, ErrNotFound
			}
			keep(op.Name)
			delete(s.records, op.Name)
			s.totalValue -= len(e.value)
			changed[op.Name] = Record{Name: op.Name, Revision: e.revision}
		}
	}

	if len(s.records) > s.opts.MaxRecords || s.totalValue > s.opts.MaxTotalValueBytes {
		restore()
		return Result{}, ErrCapacity
	}

	s.generation++
	out := make([]Record, 0, len(changed))
	for _, r := range changed {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Result{Generation: s.generation, Revision: lastRev, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: name, Value: v, Revision: e.revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
