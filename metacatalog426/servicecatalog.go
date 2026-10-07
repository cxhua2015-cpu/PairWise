package metacatalog426

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
	mu           sync.Mutex
	opts         Options
	records      map[string][]byte
	revision     map[string]uint64
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:         o,
		records:      make(map[string][]byte),
		revision:     make(map[string]uint64),
		nextRevision: 1,
	}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records := make(map[string][]byte, len(s.records))
	for k, v := range s.records {
		records[k] = v
	}
	revision := make(map[string]uint64, len(s.revision))
	for k, v := range s.revision {
		revision[k] = v
	}
	nextRevision := s.nextRevision
	changed := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			records[op.Name] = v
			revision[op.Name] = nextRevision
			changed[op.Name] = Record{Name: op.Name, Value: v, Revision: nextRevision}
			nextRevision++
		case Delete:
			if _, ok := records[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(records, op.Name)
			delete(revision, op.Name)
			delete(changed, op.Name)
		}
	}

	total := 0
	for _, v := range records {
		total += len(v)
	}
	if len(records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = records
	s.revision = revision
	s.nextRevision = nextRevision

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: nextRevision - 1, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		r := changed[name]
		out.Changed = append(out.Changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return out, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
		return Record{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), v...), Revision: s.revision[name]}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		snap.Records = append(snap.Records, Record{Name: name, Value: append([]byte(nil), s.records[name]...), Revision: s.revision[name]})
	}
	return snap
}
