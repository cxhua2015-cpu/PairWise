package modelcatalog

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
	records      map[string]Record
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record), nextRevision: 1}, nil
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

func cloneValue(v []byte) []byte {
	if v == nil {
		return nil
	}
	c := make([]byte, len(v))
	copy(c, v)
	return c
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
			return Result{}, ErrCapacity
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	// Phase 2: candidate transaction on a cloned index.
	candidate := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	changed := make(map[string]Record)
	revision := s.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rec := Record{Name: op.Name, Value: cloneValue(op.Value), Revision: revision}
			revision++
			candidate[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			rec, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			changed[op.Name] = rec
		}
	}

	// Phase 3: final capacity checks at batch end.
	if len(candidate) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, rec := range candidate {
		total += len(rec.Value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.generation++
	s.nextRevision = revision

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := changed[name]
		rec.Value = cloneValue(rec.Value)
		out.Changed = append(out.Changed, rec)
	}
	return out, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	rec.Value = cloneValue(rec.Value)
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := s.records[name]
		rec.Value = cloneValue(rec.Value)
		snap.Records = append(snap.Records, rec)
	}
	return snap
}
