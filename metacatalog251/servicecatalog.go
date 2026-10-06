package metacatalog251

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
	s.mu.Lock()
	defer s.mu.Unlock()

	type backup struct {
		rec   Record
		exist bool
	}
	touched := map[string]backup{}
	save := func(name string) {
		if _, ok := touched[name]; ok {
			return
		}
		r, ok := s.records[name]
		touched[name] = backup{rec: r, exist: ok}
	}

	changed := map[string]Record{}
	rev := s.nextRevision
	var totalDelta int
	applyErr := func(err error) (Result, error) {
		for name, bk := range touched {
			if bk.exist {
				s.records[name] = bk.rec
			} else {
				delete(s.records, name)
			}
		}
		s.totalValue -= totalDelta
		return Result{}, err
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			save(op.Name)
			old, ok := s.records[op.Name]
			if ok {
				totalDelta -= len(old.Value)
			}
			v := append([]byte(nil), op.Value...)
			rec := Record{Name: op.Name, Value: v, Revision: rev}
			rev++
			totalDelta += len(v)
			s.records[op.Name] = rec
			changed[op.Name] = rec
		case Delete:
			save(op.Name)
			old, ok := s.records[op.Name]
			if !ok {
				return applyErr(ErrNotFound)
			}
			totalDelta -= len(old.Value)
			delete(s.records, op.Name)
			delete(changed, op.Name)
		}
	}

	if len(s.records) > s.opts.MaxRecords || s.totalValue+totalDelta > s.opts.MaxTotalValueBytes {
		return applyErr(ErrCapacity)
	}

	s.totalValue += totalDelta
	s.nextRevision = rev
	if len(b.Ops) > 0 {
		s.generation++
	}
	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: make([]Record, 0, len(names))}
	for _, n := range names {
		r := changed[n]
		out.Changed = append(out.Changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return out, nil
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
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}, true, nil
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
		snap.Records = append(snap.Records, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return snap
}
