package metacatalog396

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
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
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

	// Candidate transaction: clone only what the batch may touch.
	type backup struct {
		rec   Record
		exist bool
	}
	touched := make(map[string]*backup)
	lookup := func(name string) (Record, bool) {
		if bk, ok := touched[name]; ok {
			return bk.rec, bk.exist
		}
		r, ok := s.records[name]
		touched[name] = &backup{rec: r, exist: ok}
		return r, ok
	}

	candBytes := s.totalBytes
	candRev := s.revision
	changed := make(map[string]Record)
	deleted := make(map[string]bool)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, exist := lookup(op.Name)
			candRev++
			rec := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: candRev}
			if exist {
				candBytes += len(rec.Value) - len(old.Value)
			} else {
				candBytes += len(rec.Value)
			}
			touched[op.Name] = &backup{rec: rec, exist: true}
			changed[op.Name] = rec
			delete(deleted, op.Name)
		case Delete:
			old, exist := lookup(op.Name)
			if !exist {
				return Result{}, ErrNotFound
			}
			candBytes -= len(old.Value)
			touched[op.Name] = &backup{exist: false}
			delete(changed, op.Name)
			deleted[op.Name] = true
		}
	}

	// Capacity limits are checked only at the end of the batch.
	count := len(s.records)
	for name, bk := range touched {
		if bk.exist {
			if _, ok := s.records[name]; !ok {
				count++
			}
		} else {
			if _, ok := s.records[name]; ok {
				count--
			}
		}
	}
	if count > s.opts.MaxRecords || candBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, bk := range touched {
		if bk.exist {
			s.records[name] = bk.rec
		} else {
			delete(s.records, name)
		}
	}
	s.totalBytes = candBytes
	s.revision = candRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	out := make([]Record, 0, len(changed))
	for _, r := range changed {
		out = append(out, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Result{Generation: s.generation, Revision: s.revision, Changed: out}, nil
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
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
