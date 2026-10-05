package resourcecatalog131

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
	mu         sync.Mutex
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

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before any state read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	type delta struct {
		rec     Record
		deleted bool
	}
	touched := make(map[string]delta)
	total := s.totalBytes
	rev := s.revision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			old, existed := s.records[op.Name]
			if existed {
				total -= len(old.Value)
			}
			v := append([]byte(nil), op.Value...)
			touched[op.Name] = delta{rec: Record{Name: op.Name, Value: v, Revision: rev}}
			total += len(v)
		case Delete:
			old, ok := s.records[op.Name]
			if d, was := touched[op.Name]; was && !d.deleted {
				old, ok = d.rec, true
			}
			if !ok {
				return Result{}, ErrNotFound
			}
			touched[op.Name] = delta{deleted: true}
			total -= len(old.Value)
		}
	}

	// Capacity checks only at batch end.
	count := len(s.records)
	for name, d := range touched {
		if _, existed := s.records[name]; !existed && !d.deleted {
			count++
		}
		if d.deleted {
			if _, existed := s.records[name]; existed {
				count--
			}
		}
	}
	if count > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, d := range touched {
		if d.deleted {
			delete(s.records, name)
		} else {
			s.records[name] = d.rec
		}
	}
	s.totalBytes = total
	s.revision = rev
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(touched))
	for name, d := range touched {
		if !d.deleted {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), d.rec.Value...), Revision: d.rec.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: rev, Changed: changed}, nil
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
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
