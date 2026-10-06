package metacatalog286

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
	generation uint64
	revision   uint64
	totalBytes int
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record)}, nil
}

// Apply executes the batch atomically in input order. Puts allocate
// consecutive revisions; Deletes allocate none. Capacity limits are
// checked only at the end of the batch; any failure rolls back all
// state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: mutate a copy, commit by swap.
	cand := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		cand[k] = v
	}
	revision := s.revision
	totalBytes := s.totalBytes
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			var val []byte
			if op.Value != nil {
				val = make([]byte, len(op.Value))
				copy(val, op.Value)
			}
			if old, ok := cand[op.Name]; ok {
				totalBytes -= len(old.Value)
			}
			totalBytes += len(val)
			cand[op.Name] = Record{Name: op.Name, Value: val, Revision: revision}
			touched[op.Name] = struct{}{}
		case Delete:
			old, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			totalBytes -= len(old.Value)
			delete(cand, op.Name)
			delete(touched, op.Name)
		}
	}

	if len(cand) > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = cand
	s.revision = revision
	s.totalBytes = totalBytes
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		changed = append(changed, cloneRecord(cand[name]))
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(r), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, cloneRecord(r))
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}

func cloneRecord(r Record) Record {
	if r.Value != nil {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		r.Value = v
	}
	return r
}
