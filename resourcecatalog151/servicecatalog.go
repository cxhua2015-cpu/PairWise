package resourcecatalog151

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
	mu           sync.RWMutex
	opts         Options
	records      map[string]record
	generation   uint64
	nextRevision uint64
	totalBytes   int
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
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

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]record), nextRevision: 1}, nil
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
		if op.Kind == Delete && len(op.Value) != 0 {
			return Result{}, ErrInvalidInput
		}
		if len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	// Phase 2: execute on a candidate transaction; commit only on success.
	candidate := make(map[string]record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	totalBytes := s.totalBytes
	nextRevision := s.nextRevision
	touched := make(map[string]bool)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := candidate[op.Name]
			if existed {
				totalBytes -= len(old.value)
			}
			cp := make([]byte, len(op.Value))
			copy(cp, op.Value)
			candidate[op.Name] = record{value: cp, revision: nextRevision}
			totalBytes += len(cp)
			nextRevision++
			touched[op.Name] = true
		case Delete:
			old, existed := candidate[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			totalBytes -= len(old.value)
			delete(candidate, op.Name)
			touched[op.Name] = true
		}
	}

	// Phase 3: capacity limits checked only at batch end.
	if len(candidate) > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalBytes = totalBytes
	s.generation++
	s.nextRevision = nextRevision

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := candidate[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: nextRevision - 1, Changed: changed}, nil
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
	return Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for name, r := range s.records {
		records = append(records, Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
