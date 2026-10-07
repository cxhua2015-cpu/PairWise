package metacatalog421

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
type Store struct {
	mu         sync.RWMutex
	opts       Options
	records    map[string]Record
	totalBytes int
	generation uint64
	nextRev    uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyLocked(b)
}

// applyLocked executes the batch against a candidate state and commits only on
// full success, so any failure leaves state, generation and revision untouched.
func (s *Store) applyLocked(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.lastRevision()}, nil
	}
	records := make(map[string]Record, len(s.records)+len(b.Ops))
	for name, rec := range s.records {
		records[name] = rec
	}
	total := s.totalBytes
	nextRev := s.nextRev
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := append([]byte(nil), op.Value...)
			if old, ok := records[op.Name]; ok {
				total -= len(old.Value)
			}
			total += len(value)
			rec := Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
			records[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			old, ok := records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(records, op.Name)
			delete(changed, op.Name)
		}
	}
	if len(records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}
	s.records = records
	s.totalBytes = total
	s.nextRev = nextRev
	s.generation++
	result := Result{Generation: s.generation, Revision: nextRev - 1}
	if len(changed) > 0 {
		names := make([]string, 0, len(changed))
		for name := range changed {
			names = append(names, name)
		}
		sort.Strings(names)
		result.Changed = make([]Record, 0, len(names))
		for _, name := range names {
			rec := changed[name]
			result.Changed = append(result.Changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
		}
	}
	return result, nil
}

func (s *Store) lastRevision() uint64 { return s.nextRev - 1 }

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRev}
	if len(names) > 0 {
		snap.Records = make([]Record, 0, len(names))
		for _, name := range names {
			rec := s.records[name]
			snap.Records = append(snap.Records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
		}
	}
	return snap
}
