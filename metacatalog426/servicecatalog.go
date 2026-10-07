package metacatalog426

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
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.applyLocked(b)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// applyLocked executes the batch against current state, rolling back fully on
// any failure. Callers must hold s.mu and must have validated the batch.
func (s *Store) applyLocked(b Batch) (Result, error) {
	type backup struct {
		rec     Record
		existed bool
	}
	touched := make(map[string]backup)
	changed := make(map[string]Record)
	var lastRev uint64
	origNextRevision := s.nextRevision

	restore := func() {
		for name, bak := range touched {
			if bak.existed {
				s.records[name] = bak.rec
			} else {
				delete(s.records, name)
			}
		}
		s.nextRevision = origNextRevision
	}

	for _, op := range b.Ops {
		if _, seen := touched[op.Name]; !seen {
			rec, existed := s.records[op.Name]
			touched[op.Name] = backup{rec: rec, existed: existed}
		}
		switch op.Kind {
		case Put:
			rec := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: s.nextRevision}
			lastRev = s.nextRevision
			s.nextRevision++
			s.records[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			if _, ok := s.records[op.Name]; !ok {
				restore()
				return Result{}, ErrNotFound
			}
			delete(s.records, op.Name)
			delete(changed, op.Name)
		}
	}

	total := 0
	for _, rec := range s.records {
		total += len(rec.Value)
	}
	if len(s.records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		restore()
		return Result{}, ErrCapacity
	}

	s.generation++
	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Record, 0, len(names))
	for _, name := range names {
		rec := changed[name]
		out = append(out, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	return Result{Generation: s.generation, Revision: lastRev, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
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
	recs := make([]Record, 0, len(names))
	for _, name := range names {
		rec := s.records[name]
		recs = append(recs, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
