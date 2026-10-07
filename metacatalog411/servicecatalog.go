package metacatalog411

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
	totalValue   int
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record), nextRevision: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	type change struct {
		rec     Record
		deleted bool
	}
	changed := make(map[string]change)
	nextRev := s.nextRevision
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rec := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: nextRev}
			lastRev = nextRev
			nextRev++
			changed[op.Name] = change{rec: rec}
		case Delete:
			if _, ok := s.records[op.Name]; !ok {
				if _, pending := changed[op.Name]; !pending {
					return Result{}, ErrNotFound
				}
			}
			changed[op.Name] = change{deleted: true}
		}
	}

	// Apply to a candidate view and check final capacity at batch end.
	candidate := make(map[string]Record, len(s.records)+len(changed))
	total := 0
	for name, rec := range s.records {
		candidate[name] = rec
		total += len(rec.Value)
	}
	for name, c := range changed {
		if old, ok := candidate[name]; ok {
			total -= len(old.Value)
		}
		if c.deleted {
			delete(candidate, name)
		} else {
			candidate[name] = c.rec
			total += len(c.rec.Value)
		}
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(changed))
	for name, c := range changed {
		if !c.deleted {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	res := Result{Generation: s.generation, Revision: lastRev, Changed: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := s.records[name]
		res.Changed = append(res.Changed, Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision})
	}
	return res, nil
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
	return Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(s.records))}
	for _, rec := range s.records {
		snap.Records = append(snap.Records, Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision})
	}
	sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	return snap
}
