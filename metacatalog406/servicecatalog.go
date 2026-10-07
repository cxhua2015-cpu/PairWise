package metacatalog406

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
	mu      sync.RWMutex
	opts    Options
	records map[string]Record
	total   int
	gen     uint64
	nextRev uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	type undo struct {
		name    string
		rec     Record
		existed bool
	}
	var undos []undo
	seen := map[string]bool{}
	backup := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		r, ok := s.records[name]
		undos = append(undos, undo{name, r, ok})
	}
	rollback := func() {
		for _, u := range undos {
			if u.existed {
				s.records[u.name] = u.rec
			} else {
				delete(s.records, u.name)
			}
		}
	}

	startRev := s.nextRev
	startTotal := s.total
	touched := map[string]bool{}

	for _, op := range b.Ops {
		backup(op.Name)
		touched[op.Name] = true
		switch op.Kind {
		case Put:
			if old, ok := s.records[op.Name]; ok {
				s.total -= len(old.Value)
			}
			v := append([]byte(nil), op.Value...)
			s.records[op.Name] = Record{Name: op.Name, Value: v, Revision: s.nextRev}
			s.total += len(v)
			s.nextRev++
		case Delete:
			r, ok := s.records[op.Name]
			if !ok {
				rollback()
				s.nextRev = startRev
				s.total = startTotal
				return Result{}, ErrNotFound
			}
			s.total -= len(r.Value)
			delete(s.records, op.Name)
		}
	}

	if len(s.records) > s.opts.MaxRecords || s.total > s.opts.MaxTotalValueBytes {
		rollback()
		s.nextRev = startRev
		s.total = startTotal
		return Result{}, ErrCapacity
	}

	res := Result{Generation: s.gen, Revision: s.nextRev - 1}
	if len(b.Ops) > 0 {
		s.gen++
		res.Generation = s.gen
	}
	names := make([]string, 0, len(touched))
	for n := range touched {
		if _, ok := s.records[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		r := s.records[n]
		res.Changed = append(res.Changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return res, nil
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
	snap := Snapshot{Generation: s.gen, NextRevision: s.nextRev, Records: make([]Record, 0, len(s.records))}
	for _, r := range s.records {
		snap.Records = append(snap.Records, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	return snap
}
