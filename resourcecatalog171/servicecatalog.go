package resourcecatalog171

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

type entry struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu sync.RWMutex

	opts Options

	records map[string]entry

	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 ||
		opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		opts:         opts,
		records:      make(map[string]entry),
		nextRevision: 1,
	}, nil
}

// validName reports whether name is a non-empty sequence of ASCII lowercase
// letters, digits, '-' or '_' whose byte length fits within maxNameBytes.
func validName(name string, maxNameBytes int) bool {
	if len(name) == 0 || len(name) > maxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// validateBatch structurally validates every op before any state is read.
func validateBatch(b Batch, opts Options) error {
	for _, op := range b.Ops {
		if !validName(op.Name, opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if len(op.Value) != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// Apply executes a batch atomically. All ops are structurally validated before
// any state is read; the batch is then applied to a private candidate copy.
// Revision numbers are allocated contiguously, one per Put (Delete allocates
// none). The final record count and total value capacity are verified only at
// batch end. On any failure every state change, including generation and the
// revision counter, is rolled back by simply discarding the candidate.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := validateBatch(b, s.opts); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cand := make(map[string]entry, len(s.records)+len(b.Ops))
	for name, e := range s.records {
		cand[name] = entry{value: e.value, revision: e.revision}
	}

	rev := s.nextRevision
	var lastRevision uint64
	touched := make(map[string]struct{}, len(b.Ops))

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			stored := make([]byte, len(op.Value))
			copy(stored, op.Value)
			cand[op.Name] = entry{value: stored, revision: rev}
			lastRevision = rev
			rev++
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	var totalValueBytes int
	for _, e := range cand {
		totalValueBytes += len(e.value)
	}
	if len(cand) > s.opts.MaxRecords || totalValueBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.generation++
	}
	s.nextRevision = rev
	s.records = cand

	changed := make([]Record, 0, len(touched))
	for name := range touched {
		e, ok := cand[name]
		if !ok {
			continue
		}
		changed = append(changed, Record{
			Name:     name,
			Value:    cloneBytes(e.value),
			Revision: e.revision,
		})
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{
		Generation: s.generation,
		Revision:   lastRevision,
		Changed:    changed,
	}, nil
}

// Get returns a deep copy of the named record. A missing record reports
// (zero Record, false, nil).
func (s *Store) Get(name string) (Record, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{
		Name:     name,
		Value:    cloneBytes(e.value),
		Revision: e.revision,
	}, true, nil
}

// Snapshot returns an isolated, name-sorted copy of the whole catalog.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, 0, len(s.records))
	for name, e := range s.records {
		records = append(records, Record{
			Name:     name,
			Value:    cloneBytes(e.value),
			Revision: e.revision,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })

	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Records:      records,
	}
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
