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

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
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
		switch op.Kind {
		case Put:
			if len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if len(op.Value) != 0 {
				return ErrInvalidInput
			}
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

	// Candidate transaction: clone-on-write for touched names only.
	type backup struct {
		e      entry
		exists bool
	}
	touched := make(map[string]backup)
	totalBytes := s.totalBytes
	revision := s.revision
	changed := make(map[string]entry)
	removed := make(map[string]bool)

	restore := func() {
		for name, bak := range touched {
			if bak.exists {
				s.records[name] = bak.e
			} else {
				delete(s.records, name)
			}
		}
	}

	for _, op := range b.Ops {
		if _, ok := touched[op.Name]; !ok {
			e, exists := s.records[op.Name]
			touched[op.Name] = backup{e: e, exists: exists}
		}
		switch op.Kind {
		case Put:
			revision++
			old, existed := s.records[op.Name]
			if existed {
				totalBytes -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			s.records[op.Name] = entry{value: v, revision: revision}
			totalBytes += len(v)
			changed[op.Name] = s.records[op.Name]
			delete(removed, op.Name)
		case Delete:
			old, existed := s.records[op.Name]
			if !existed {
				restore()
				return Result{}, ErrNotFound
			}
			delete(s.records, op.Name)
			totalBytes -= len(old.value)
			delete(changed, op.Name)
			removed[op.Name] = true
		}
	}

	if len(s.records) > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		restore()
		return Result{}, ErrCapacity
	}

	s.totalBytes = totalBytes
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	res := Result{Generation: s.generation, Revision: s.revision}
	for _, name := range names {
		e := changed[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		res.Changed = append(res.Changed, Record{Name: name, Value: v, Revision: e.revision})
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
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	for _, name := range names {
		e := s.records[name]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		snap.Records = append(snap.Records, Record{Name: name, Value: v, Revision: e.revision})
	}
	return snap
}
