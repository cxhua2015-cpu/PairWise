// Package casstore implements a concurrency-safe in-memory
// compare-and-swap transactional key-value store. See SPEC.md.
package casstore

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type CompareKind uint8

const (
	Exists CompareKind = iota + 1
	NotExists
	Revision
	Value
)

type WriteKind uint8

const (
	Put WriteKind = iota + 1
	Delete
)

type Options struct{ MaxKeys, MaxValueBytes, MaxNameBytes int }
type Compare struct {
	Kind     CompareKind
	Key      string
	Revision uint64
	Value    []byte
}
type Write struct {
	Kind  WriteKind
	Key   string
	Value []byte
}
type Txn struct {
	Compares []Compare
	Writes   []Write
}
type Entry struct {
	Key      string
	Revision uint64
	Value    []byte
}
type TxnResult struct {
	Succeeded            bool
	Generation, Revision uint64
}
type Snapshot struct {
	Generation, NextRevision uint64
	Keys, ValueBytes         int
	Entries                  []Entry
}

type Store struct {
	mu           sync.Mutex
	maxKeys      int
	maxValueByte int
	maxNameBytes int
	entries      map[string]Entry
	valueBytes   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxKeys <= 0 || o.MaxValueBytes <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxKeys:      o.MaxKeys,
		maxValueByte: o.MaxValueBytes,
		maxNameBytes: o.MaxNameBytes,
		entries:      make(map[string]Entry),
		nextRevision: 1,
	}, nil
}

func validKeyChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '.', '_', '/', '-':
		return true
	}
	return false
}

func (s *Store) validKey(k string) bool {
	if len(k) == 0 || len(k) > s.maxNameBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		if !validKeyChar(k[i]) {
			return false
		}
	}
	return true
}

func (s *Store) validateCompare(c Compare) error {
	if !s.validKey(c.Key) {
		return ErrInvalidInput
	}
	switch c.Kind {
	case Exists, NotExists:
		if c.Revision != 0 || c.Value != nil {
			return ErrInvalidInput
		}
	case Revision:
		if c.Revision == 0 || c.Value != nil {
			return ErrInvalidInput
		}
	case Value:
		if c.Revision != 0 || c.Value == nil || len(c.Value) > s.maxValueByte {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) validateWrite(w Write) error {
	if !s.validKey(w.Key) {
		return ErrInvalidInput
	}
	switch w.Kind {
	case Put:
		if w.Value == nil || len(w.Value) > s.maxValueByte {
			return ErrInvalidInput
		}
	case Delete:
		if w.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func cloneEntry(e Entry) Entry {
	e.Value = cloneBytes(e.Value)
	return e
}

func (s *Store) Transact(t Txn) (TxnResult, error) {
	// Structural validation of all compares, then all writes,
	// before any state is read.
	for _, c := range t.Compares {
		if err := s.validateCompare(c); err != nil {
			return TxnResult{}, err
		}
	}
	for _, w := range t.Writes {
		if err := s.validateWrite(w); err != nil {
			return TxnResult{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Compares evaluate against the snapshot at transaction start.
	for _, c := range t.Compares {
		e, ok := s.entries[c.Key]
		var pass bool
		switch c.Kind {
		case Exists:
			pass = ok
		case NotExists:
			pass = !ok
		case Revision:
			pass = ok && e.Revision == c.Revision
		case Value:
			pass = ok && string(e.Value) == string(c.Value)
		}
		if !pass {
			return TxnResult{
				Succeeded:  false,
				Generation: s.generation,
				Revision:   s.nextRevision - 1,
			}, nil
		}
	}

	if len(t.Writes) == 0 {
		return TxnResult{
			Succeeded:  true,
			Generation: s.generation,
			Revision:   s.nextRevision - 1,
		}, nil
	}

	// Apply writes sequentially on an isolated candidate.
	cand := make(map[string]Entry, len(s.entries)+len(t.Writes))
	for k, e := range s.entries {
		cand[k] = e
	}
	candBytes := s.valueBytes
	candRev := s.nextRevision
	var lastRev uint64
	for _, w := range t.Writes {
		switch w.Kind {
		case Put:
			if old, ok := cand[w.Key]; ok {
				candBytes -= len(old.Value)
			}
			v := cloneBytes(w.Value)
			cand[w.Key] = Entry{Key: w.Key, Revision: candRev, Value: v}
			candBytes += len(v)
			lastRev = candRev
			candRev++
		case Delete:
			old, ok := cand[w.Key]
			if !ok {
				return TxnResult{}, ErrNotFound
			}
			delete(cand, w.Key)
			candBytes -= len(old.Value)
		}
	}

	// Final capacity check: key count and total live value bytes.
	if len(cand) > s.maxKeys || candBytes > s.maxKeys*s.maxValueByte {
		return TxnResult{}, ErrCapacity
	}

	s.entries = cand
	s.valueBytes = candBytes
	s.nextRevision = candRev
	s.generation++
	return TxnResult{
		Succeeded:  true,
		Generation: s.generation,
		Revision:   lastRev,
	}, nil
}

func (s *Store) Get(key string) (Entry, bool, error) {
	if !s.validKey(key) {
		return Entry{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return Entry{}, false, nil
	}
	return cloneEntry(e), true, nil
}

func (s *Store) List(prefix, after string, limit int) ([]Entry, error) {
	if prefix != "" && !s.validKey(prefix) {
		return nil, ErrInvalidInput
	}
	if after != "" && !s.validKey(after) {
		return nil, ErrInvalidInput
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, limit)
	for k, e := range s.entries {
		if k < prefix || k <= after {
			continue
		}
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		out = append(out, cloneEntry(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Keys:         len(s.entries),
		ValueBytes:   s.valueBytes,
		Entries:      make([]Entry, 0, len(s.entries)),
	}
	for _, e := range s.entries {
		snap.Entries = append(snap.Entries, cloneEntry(e))
	}
	sort.Slice(snap.Entries, func(i, j int) bool { return snap.Entries[i].Key < snap.Entries[j].Key })
	return snap
}
