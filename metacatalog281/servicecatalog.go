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

type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	totalValue   int
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
	s.mu.Lock()
	defer s.mu.Unlock()

	touched := make(map[string]recordBackup)
	changed := make(map[string]Record)
	rev := s.nextRevision
	total := s.totalValue

	for _, op := range b.Ops {
		if _, ok := touched[op.Name]; !ok {
			r, owned := s.records[op.Name]
			touched[op.Name] = recordBackup{rec: r, owned: owned}
		}
		switch op.Kind {
		case Put:
			old, existed := s.records[op.Name]
			if existed {
				total -= len(old.Value)
			}
			v := append([]byte(nil), op.Value...)
			rec := Record{Name: op.Name, Value: v, Revision: rev}
			rev++
			s.records[op.Name] = rec
			total += len(v)
			changed[op.Name] = rec
		case Delete:
			old, existed := s.records[op.Name]
			if !existed {
				s.rollback(touched)
				return Result{}, ErrNotFound
			}
			delete(s.records, op.Name)
			total -= len(old.Value)
			delete(changed, op.Name)
		}
	}

	if len(s.records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		s.rollback(touched)
		return Result{}, ErrCapacity
	}

	s.totalValue = total
	s.nextRevision = rev
	if len(b.Ops) > 0 {
		s.generation++
	}
	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := Result{Generation: s.generation, Revision: s.nextRevision - 1}
	for _, n := range names {
		r := changed[n]
		out.Changed = append(out.Changed, Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision})
	}
	return out, nil
}

type recordBackup struct {
	rec   Record
	owned bool
}

func (s *Store) rollback(touched map[string]recordBackup) {
	for name, b := range touched {
		if b.owned {
			s.records[name] = b.rec
		} else {
			delete(s.records, name)
		}
	}
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
