package resourcecatalog081

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

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Delete && op.Value != nil {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 2: candidate transaction on a copy of the index.
	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalBytes
	revision := s.revision
	changed := map[string]Record{}

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: revision}
			candidate[op.Name] = rec
			total += len(rec.Value)
			changed[op.Name] = rec
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
		}
	}

	// Phase 3: capacity limits checked only at batch end.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalBytes = total
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
		res.Changed = append(res.Changed, Record{
			Name:     name,
			Value:    cloneBytes(changed[name].Value),
			Revision: changed[name].Revision,
		})
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
	rec.Value = cloneBytes(rec.Value)
	return rec, true, nil
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
		rec := s.records[name]
		snap.Records = append(snap.Records, Record{
			Name:     rec.Name,
			Value:    cloneBytes(rec.Value),
			Revision: rec.Revision,
		})
	}
	return snap
}
