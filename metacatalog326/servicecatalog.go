package metacatalog326

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
	totalValue int
	generation uint64
	revision   uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record)}, nil
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

// validateOp performs full structural validation without reading any state.
func (s *Store) validateOp(op Op) error {
	if op.Kind != Put && op.Kind != Delete {
		return ErrInvalidInput
	}
	if !validName(op.Name, s.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Put:
		if len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	if b.Ops == nil {
		b.Ops = []Op{}
	}
	// Phase 1: full structural validation before touching any state.
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Phase 2: execute against a candidate view so failure leaves no trace.
	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	totalValue := s.totalValue
	revision := s.revision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			old, existed := candidate[op.Name]
			if existed {
				totalValue -= len(old.Value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: revision}
			totalValue += len(value)
			touched[op.Name] = struct{}{}
		case Delete:
			old, existed := candidate[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			totalValue -= len(old.Value)
			delete(candidate, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	// Phase 3: capacity invariants checked only at the end of the batch.
	if len(candidate) > s.opts.MaxRecords || totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalValue = totalValue
	s.revision = revision
	s.generation++

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		if rec, ok := candidate[name]; ok {
			changed = append(changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
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
	return Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
