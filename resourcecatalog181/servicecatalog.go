package resourcecatalog181

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

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put {
			if len(op.Value) > s.opts.MaxValueBytes {
				return Result{}, ErrInvalidInput
			}
		} else if len(op.Value) != 0 {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: v, Revision: nextRev}
			total += len(v)
			nextRev++
			touched[op.Name] = struct{}{}
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			total -= len(old.Value)
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalValue = total
	s.generation++
	s.nextRevision = nextRev

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := candidate[name]; ok {
			v := make([]byte, len(rec.Value))
			copy(v, rec.Value)
			rec.Value = v
			changed = append(changed, rec)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(rec.Value))
	copy(v, rec.Value)
	rec.Value = v
	return rec, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		v := make([]byte, len(rec.Value))
		copy(v, rec.Value)
		rec.Value = v
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}
