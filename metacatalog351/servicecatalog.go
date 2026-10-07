package metacatalog351

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
	records      map[string]Record
	totalValue   int
	generation   uint64
	nextRevision uint64
	opts         Options
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{records: make(map[string]Record), nextRevision: 1, opts: o}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
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

	// Phase 2: candidate transaction on a cloned index.
	cand := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		cand[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	changed := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := cand[op.Name]
			if existed {
				total -= len(old.Value)
			}
			rec := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: nextRev}
			nextRev++
			cand[op.Name] = rec
			total += len(rec.Value)
			changed[op.Name] = rec
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
		}
	}

	// Phase 3: final capacity checks at end of batch.
	if len(cand) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand
	s.totalValue = total
	if len(b.Ops) > 0 {
		s.generation++
	}
	s.nextRevision = nextRev

	changedList := make([]Record, 0, len(changed))
	for _, r := range changed {
		changedList = append(changedList, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(changedList, func(i, j int) bool { return changedList[i].Name < changedList[j].Name })

	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changedList}, nil
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
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
