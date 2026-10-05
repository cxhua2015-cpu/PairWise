package resourcecatalog156

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
	mu       sync.Mutex
	opts     Options
	records  map[string]Record
	revIndex map[uint64]string
	gen      uint64
	nextRev  uint64
	totalVal int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:     o,
		records:  make(map[string]Record),
		revIndex: make(map[uint64]string),
		nextRev:  1,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp performs structural validation only; it must not read store state.
func (s *Store) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.gen, Revision: s.nextRev - 1}, nil
	}

	// Phase 2: execute ops in input order against the live state, keeping an
	// undo log so any failure rolls back state, generation and revision.
	type undoEntry struct {
		name    string
		prev    Record
		existed bool
	}
	var undo []undoEntry
	changed := make(map[string]Record)
	deleted := make(map[string]bool)
	savedNextRev := s.nextRev

	rollback := func() {
		s.nextRev = savedNextRev
		for i := len(undo) - 1; i >= 0; i-- {
			u := undo[i]
			if u.existed {
				s.records[u.name] = u.prev
				s.revIndex[u.prev.Revision] = u.name
				s.totalVal += len(u.prev.Value)
			} else {
				cur := s.records[u.name]
				delete(s.records, u.name)
				delete(s.revIndex, cur.Revision)
				s.totalVal -= len(cur.Value)
			}
		}
	}

	for _, op := range b.Ops {
		prev, existed := s.records[op.Name]
		switch op.Kind {
		case Put:
			undo = append(undo, undoEntry{op.Name, prev, existed})
			rev := s.nextRev
			s.nextRev++
			val := append([]byte(nil), op.Value...)
			if existed {
				delete(s.revIndex, prev.Revision)
				s.totalVal -= len(prev.Value)
			}
			s.records[op.Name] = Record{Name: op.Name, Value: val, Revision: rev}
			s.revIndex[rev] = op.Name
			s.totalVal += len(val)
			changed[op.Name] = s.records[op.Name]
			delete(deleted, op.Name)
		case Delete:
			if !existed {
				rollback()
				return Result{}, ErrNotFound
			}
			undo = append(undo, undoEntry{op.Name, prev, true})
			delete(s.records, op.Name)
			delete(s.revIndex, prev.Revision)
			s.totalVal -= len(prev.Value)
			delete(changed, op.Name)
			deleted[op.Name] = true
		}
	}

	// Phase 3: capacity limits are checked only at the end of the batch.
	if len(s.records) > s.opts.MaxRecords || s.totalVal > s.opts.MaxTotalValueBytes {
		rollback()
		return Result{}, ErrCapacity
	}

	s.gen++
	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Record, 0, len(names))
	for _, n := range names {
		r := changed[n]
		out = append(out, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return Result{Generation: s.gen, Revision: s.nextRev - 1, Changed: out}, nil
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
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		r := s.records[n]
		recs = append(recs, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return Snapshot{Generation: s.gen, NextRevision: s.nextRev, Records: recs}
}
