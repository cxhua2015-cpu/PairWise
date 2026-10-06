package metacatalog296

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
	records      map[string][]byte
	revisions    map[string]uint64
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
		revisions:    make(map[string]uint64),
		nextRevision: 1,
	}, nil
}

// Apply executes a batch atomically in input order. Put allocates consecutive
// revisions; Delete does not. Capacity limits are only checked at batch end.
// Any failure rolls back all state, generation and revision.
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
	revisions := make(map[string]uint64, len(s.revisions))
	for k, v := range s.revisions {
		revisions[k] = v
	}
	nextRevision := s.nextRevision

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			records[op.Name] = v
			revisions[op.Name] = nextRevision
			nextRevision++
		case Delete:
			if _, ok := records[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(records, op.Name)
			delete(revisions, op.Name)
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
	s.revisions = revisions
	s.nextRevision = nextRevision

	names := make([]string, 0, len(records))
	seen := make(map[string]bool, len(b.Ops))
	for _, op := range b.Ops {
		if !seen[op.Name] {
			seen[op.Name] = true
			if _, ok := records[op.Name]; ok {
				names = append(names, op.Name)
			}
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, n := range names {
		v := make([]byte, len(records[n]))
		copy(v, records[n])
		changed = append(changed, Record{Name: n, Value: v, Revision: revisions[n]})
	}
	return Result{Generation: s.generation, Revision: nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return Record{Name: name, Value: out, Revision: s.revisions[name]}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		v := make([]byte, len(s.records[n]))
		copy(v, s.records[n])
		recs = append(recs, Record{Name: n, Value: v, Revision: s.revisions[n]})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
