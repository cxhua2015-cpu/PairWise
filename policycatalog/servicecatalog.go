package policycatalog

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

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]Record)}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validNameForRead(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching any state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) > s.opts.MaxValueBytes {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if !validName(op.Name, s.opts.MaxNameBytes) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Phase 2: candidate transaction on a cloned state.
	records := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		records[k] = v
	}
	totalValue := s.totalValue
	revision := s.revision

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			old, existed := records[op.Name]
			if existed {
				totalValue -= len(old.Value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			records[op.Name] = Record{Name: op.Name, Value: value, Revision: revision}
			totalValue += len(value)
		case Delete:
			old, existed := records[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(records, op.Name)
			totalValue -= len(old.Value)
		}
	}

	// Phase 3: final capacity checks at end of batch.
	if len(records) > s.opts.MaxRecords || totalValue > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = records
	s.totalValue = totalValue
	s.revision = revision
	s.generation++

	changed := make([]Record, 0, len(b.Ops))
	seen := make(map[string]struct{}, len(b.Ops))
	for _, op := range b.Ops {
		if _, ok := seen[op.Name]; ok {
			continue
		}
		seen[op.Name] = struct{}{}
		if rec, ok := records[op.Name]; ok {
			changed = append(changed, Record{Name: rec.Name, Value: append([]byte(nil), rec.Value...), Revision: rec.Revision})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validNameForRead(name) {
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
