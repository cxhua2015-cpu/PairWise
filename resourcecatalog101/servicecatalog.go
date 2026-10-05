package resourcecatalog101

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

func validName(n string, max int) bool {
	if len(n) == 0 || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func cloneValue(v []byte) []byte {
	if v == nil {
		return nil
	}
	c := make([]byte, len(v))
	copy(c, v)
	return c
}

func (s *Store) Apply(b Batch) (Result, error) {
	// 1. Full structural validation before touching any state.
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

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}

	// 2. Candidate transaction: apply ops in input order on a copy-on-write overlay.
	type delta struct {
		rec     Record
		exists  bool
		deleted bool
	}
	changed := make(map[string]*delta)
	order := []string{}
	getDelta := func(name string) *delta {
		if d, ok := changed[name]; ok {
			return d
		}
		rec, ok := s.records[name]
		d := &delta{rec: rec, exists: ok}
		changed[name] = d
		order = append(order, name)
		return d
	}

	rev := s.nextRevision
	for _, op := range b.Ops {
		d := getDelta(op.Name)
		switch op.Kind {
		case Put:
			d.rec = Record{Name: op.Name, Value: cloneValue(op.Value), Revision: rev}
			d.exists = true
			d.deleted = false
			rev++
		case Delete:
			if !d.exists {
				return Result{}, ErrNotFound
			}
			d.exists = false
			d.deleted = true
		}
	}

	// 3. Final capacity checks at end of batch.
	finalRecords := len(s.records)
	finalTotal := s.totalValue
	for _, name := range order {
		d := changed[name]
		_, prior := s.records[name]
		switch {
		case prior && d.exists:
			finalTotal += len(d.rec.Value) - len(s.records[name].Value)
		case prior && !d.exists:
			finalRecords--
			finalTotal -= len(s.records[name].Value)
		case !prior && d.exists:
			finalRecords++
			finalTotal += len(d.rec.Value)
		}
	}
	if finalRecords > s.opts.MaxRecords || finalTotal > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// 4. Commit.
	for _, name := range order {
		d := changed[name]
		if d.exists {
			s.records[name] = d.rec
		} else {
			delete(s.records, name)
		}
	}
	s.totalValue = finalTotal
	s.generation++
	s.nextRevision = rev

	out := Result{Generation: s.generation, Revision: rev - 1}
	names := make([]string, 0, len(order))
	for _, name := range order {
		if changed[name].exists {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		rec := s.records[name]
		out.Changed = append(out.Changed, Record{Name: rec.Name, Value: cloneValue(rec.Value), Revision: rec.Revision})
	}
	return out, nil
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
	return Record{Name: rec.Name, Value: cloneValue(rec.Value), Revision: rec.Revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		rec := s.records[name]
		snap.Records = append(snap.Records, Record{Name: rec.Name, Value: cloneValue(rec.Value), Revision: rec.Revision})
	}
	return snap
}
