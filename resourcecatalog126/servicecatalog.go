package resourcecatalog126

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
	mu         sync.Mutex
	opts       Options
	records    map[string]Record
	totalValue int
	generation uint64
	revision   uint64
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record)}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 2: execute ops in input order against a candidate view.
	type candidate struct {
		records    map[string]Record
		totalValue int
		revision   uint64
	}
	cand := candidate{
		records:    make(map[string]Record, len(s.records)),
		totalValue: s.totalValue,
		revision:   s.revision,
	}
	for name, rec := range s.records {
		cand.records[name] = rec
	}

	final := make(map[string]Record)
	order := make([]string, 0, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand.revision++
			rec := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: cand.revision}
			if old, ok := cand.records[op.Name]; ok {
				cand.totalValue -= len(old.Value)
			}
			cand.totalValue += len(rec.Value)
			cand.records[op.Name] = rec
			if _, seen := final[op.Name]; !seen {
				order = append(order, op.Name)
			}
			final[op.Name] = rec
		case Delete:
			old, ok := cand.records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			cand.totalValue -= len(old.Value)
			delete(cand.records, op.Name)
			delete(final, op.Name)
		}
	}

	// Phase 3: capacity checks only at batch end.
	if len(cand.records) > s.opts.MaxRecords || cand.totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = cand.records
	s.totalValue = cand.totalValue
	s.revision = cand.revision
	if len(b.Ops) > 0 {
		s.generation++
	}

	changed := make([]Record, 0, len(final))
	for _, name := range order {
		if rec, ok := final[name]; ok {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]Record, 0, len(s.records))
	for _, rec := range s.records {
		records = append(records, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: records}
}
