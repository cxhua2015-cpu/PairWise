package metacatalog431

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

// state is the internal mutable model shared by the live store, clones and
// preview candidates. records maps name to its current record.
type state struct {
	generation      uint64
	nextRevision    uint64
	records         map[string]Record
	totalValueBytes int
}

func newState() state {
	return state{nextRevision: 1, records: make(map[string]Record)}
}

// snapshotLocked builds a sorted, ownership-isolated snapshot of st.
func (st *state) snapshot() Snapshot {
	snap := Snapshot{Generation: st.generation, NextRevision: st.nextRevision}
	snap.Records = make([]Record, 0, len(st.records))
	for _, r := range st.records {
		snap.Records = append(snap.Records, cloneRecord(r))
	}
	sort.Slice(snap.Records, func(i, j int) bool { return snap.Records[i].Name < snap.Records[j].Name })
	return snap
}

// applyBatch executes the full transaction semantics against st: structural
// validation first, then in-order execution, then end-of-batch capacity
// checks. On any error st is left untouched.
func (st *state) applyBatch(opts Options, b Batch) (Result, error) {
	if err := validateBatch(opts, b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: st.generation, Revision: st.nextRevision - 1}, nil
	}

	// Work on an isolated candidate so failures roll back for free.
	cand := st.cloneState()
	touched := make(map[string]struct{})
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rec := Record{Name: op.Name, Value: cloneBytes(op.Value), Revision: cand.nextRevision}
			cand.nextRevision++
			if old, ok := cand.records[op.Name]; ok {
				cand.totalValueBytes -= len(old.Value)
			}
			cand.records[op.Name] = rec
			cand.totalValueBytes += len(rec.Value)
		case Delete:
			old, ok := cand.records[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand.records, op.Name)
			cand.totalValueBytes -= len(old.Value)
		}
		touched[op.Name] = struct{}{}
	}

	// Final capacity checks happen only at the end of the batch.
	if len(cand.records) > opts.MaxRecords || cand.totalValueBytes > opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	cand.generation++
	*st = cand

	res := Result{Generation: st.generation, Revision: st.nextRevision - 1}
	for name := range touched {
		if rec, ok := st.records[name]; ok {
			res.Changed = append(res.Changed, cloneRecord(rec))
		}
	}
	sort.Slice(res.Changed, func(i, j int) bool { return res.Changed[i].Name < res.Changed[j].Name })
	return res, nil
}

func (st *state) cloneState() state {
	out := state{
		generation:      st.generation,
		nextRevision:    st.nextRevision,
		records:         make(map[string]Record, len(st.records)),
		totalValueBytes: st.totalValueBytes,
	}
	for name, rec := range st.records {
		out.records[name] = cloneRecord(rec)
	}
	return out
}

func cloneRecord(r Record) Record {
	return Record{Name: r.Name, Value: cloneBytes(r.Value), Revision: r.Revision}
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// Store is a concurrency-safe in-memory metadata catalog.
type Store struct {
	mu    sync.RWMutex
	opts  Options
	state state
}

func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, state: newState()}, nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.applyBatch(s.opts, b)
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(s.opts, name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.state.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.snapshot()
}
