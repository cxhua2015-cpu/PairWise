package metacatalog416

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
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}
	// Stage changes on a private copy so failure rolls back everything.
	records := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		records[k] = v
	}
	total := s.totalValue
	rev := s.nextRevision
	touched := map[string]struct{}{}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := records[op.Name]; ok {
				total -= len(old.Value)
			}
			records[op.Name] = Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: rev}
			total += len(op.Value)
			rev++
			touched[op.Name] = struct{}{}
		case Delete:
			old, ok := records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(records, op.Name)
			touched[op.Name] = struct{}{}
		}
	}
	if len(records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}
	s.records = records
	s.totalValue = total
	s.generation++
	s.nextRevision = rev
	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if r, ok := records[name]; ok {
			changed = append(changed, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: s.generation, Revision: rev - 1, Changed: changed}, nil
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
	return Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
