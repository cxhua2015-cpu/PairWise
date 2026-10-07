package metacatalog436

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
	nextRev    uint64
	totalBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

// applyLocked executes an already validated batch against m, which must be a
// private working copy. It returns the changed records (final state for puts,
// pre-delete state for deletes) sorted by name.
func applyLocked(m map[string]Record, b Batch, nextRev uint64) (changed []Record, rev uint64, err error) {
	seen := map[string]bool{}
	rev = nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			m[op.Name] = Record{Name: op.Name, Value: v, Revision: rev}
			rev++
			seen[op.Name] = true
		case Delete:
			old, ok := m[op.Name]
			if !ok {
				return nil, 0, ErrNotFound
			}
			delete(m, op.Name)
			if !seen[op.Name] {
				seen[op.Name] = true
				changed = append(changed, old)
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if r, ok := m[n]; ok {
			changed = append(changed, r)
		}
	}
	return changed, rev, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	work := make(map[string]Record, len(s.records))
	for k, r := range s.records {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		work[k] = Record{Name: r.Name, Value: v, Revision: r.Revision}
	}
	changed, rev, err := applyLocked(work, b, s.nextRev)
	if err != nil {
		return Result{}, err
	}
	total := 0
	for _, r := range work {
		total += len(r.Value)
	}
	if len(work) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}
	s.records = work
	s.totalBytes = total
	if len(b.Ops) > 0 {
		s.generation++
	}
	s.nextRev = rev
	lastRev := rev - 1
	if rev == 0 {
		lastRev = 0
	}
	return Result{Generation: s.generation, Revision: lastRev, Changed: changed}, nil
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
	v := make([]byte, len(r.Value))
	copy(v, r.Value)
	return Record{Name: r.Name, Value: v, Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	recs := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		v := make([]byte, len(r.Value))
		copy(v, r.Value)
		recs = append(recs, Record{Name: r.Name, Value: v, Revision: r.Revision})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRev, Records: recs}
}
