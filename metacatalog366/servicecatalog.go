package metacatalog366

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

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching any state.
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

	// Phase 2: apply ops in input order on a candidate view so failures
	// roll back records, generation and revision automatically.
	type entry struct {
		rec     Record
		exists  bool
		deleted bool
	}
	candidate := make(map[string]entry, len(b.Ops))
	totalBytes := s.totalBytes
	revision := s.revision
	changedIdx := make(map[string]int)
	var changed []Record

	lookup := func(name string) (Record, bool) {
		if e, ok := candidate[name]; ok {
			return e.rec, e.exists && !e.deleted
		}
		r, ok := s.records[name]
		return r, ok
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			rec := Record{Name: op.Name, Value: value, Revision: revision}
			if old, ok := lookup(op.Name); ok {
				totalBytes -= len(old.Value)
			}
			totalBytes += len(value)
			if e, ok := candidate[op.Name]; ok {
				e.rec, e.exists, e.deleted = rec, true, false
				candidate[op.Name] = e
			} else {
				candidate[op.Name] = entry{rec: rec, exists: true}
			}
			if i, ok := changedIdx[op.Name]; ok {
				changed[i] = rec
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, rec)
			}
		case Delete:
			if _, ok := lookup(op.Name); !ok {
				return Result{}, ErrNotFound
			}
			old, _ := lookup(op.Name)
			totalBytes -= len(old.Value)
			if e, ok := candidate[op.Name]; ok {
				e.deleted = true
				candidate[op.Name] = e
			} else {
				candidate[op.Name] = entry{deleted: true}
			}
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for n, j := range changedIdx {
					if j > i {
						changedIdx[n] = j - 1
					}
				}
			}
		}
	}

	// Phase 3: capacity limits checked only at the end of the batch.
	recordCount := len(s.records)
	for name, e := range candidate {
		_, committed := s.records[name]
		switch {
		case e.deleted && committed:
			recordCount--
		case !e.deleted && e.exists && !committed:
			recordCount++
		}
	}
	if recordCount > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, e := range candidate {
		if e.deleted {
			delete(s.records, name)
		} else if e.exists {
			s.records[name] = e.rec
		}
	}
	s.totalBytes = totalBytes
	s.revision = revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
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
	r.Value = append([]byte(nil), r.Value...)
	return r, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		r.Value = append([]byte(nil), r.Value...)
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
