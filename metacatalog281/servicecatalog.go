package metacatalog281

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

// Store is a concurrency-safe in-memory metadata catalog.
//
// Internally it keeps a name-indexed map of records plus two logical
// clocks: generation (bumped once per non-empty successful batch) and
// nextRevision (the revision the next Put will receive). A single
// RWMutex linearizes all public operations.
type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
	totalBytes   int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. It first runs the
// shared structural precheck (no state reads), then replays the ops on a
// candidate transaction; capacity limits are only checked at the end. Any
// failure discards the candidate, leaving state, generation and revision
// untouched.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cand := s.candidateLocked()
	changed := make(map[string]Record)
	revision := s.nextRevision - 1
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev := cand.nextRevision
			cand.nextRevision++
			revision = rev
			v := append([]byte(nil), op.Value...)
			if old, ok := cand.records[op.Name]; ok {
				cand.totalBytes -= len(old.Value)
			}
			cand.records[op.Name] = Record{Name: op.Name, Value: v, Revision: rev}
			cand.totalBytes += len(v)
			changed[op.Name] = cand.records[op.Name]
		case Delete:
			old, ok := cand.records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			cand.totalBytes -= len(old.Value)
			delete(cand.records, op.Name)
			changed[op.Name] = Record{Name: op.Name, Revision: old.Revision}
		}
	}
	if len(cand.records) > s.opts.MaxRecords || cand.totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	res := Result{Generation: s.generation, Revision: revision}
	if len(b.Ops) > 0 {
		res.Generation = s.generation + 1
		s.generation = res.Generation
		s.records = cand.records
		s.nextRevision = cand.nextRevision
		s.totalBytes = cand.totalBytes
		names := make([]string, 0, len(changed))
		for n := range changed {
			names = append(names, n)
		}
		sort.Strings(names)
		res.Changed = make([]Record, 0, len(names))
		for _, n := range names {
			r := changed[n]
			r.Value = append([]byte(nil), r.Value...)
			res.Changed = append(res.Changed, r)
		}
	}
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
		return Record{}, false, err
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
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, n := range names {
		r := s.records[n]
		r.Value = append([]byte(nil), r.Value...)
		snap.Records = append(snap.Records, r)
	}
	return snap
}

// candidateLocked copies the committed state into a scratch transaction.
// The caller must hold s.mu. Record values are shared until a Put replaces
// them, which is safe because committed values are never mutated in place.
func (s *Store) candidateLocked() *Store {
	c := &Store{
		records:      make(map[string]Record, len(s.records)),
		nextRevision: s.nextRevision,
		totalBytes:   s.totalBytes,
	}
	for n, r := range s.records {
		c.records[n] = r
	}
	return c
}
