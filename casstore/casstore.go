package casstore

import (
	"bytes"
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
	mu           sync.RWMutex
	maxKeys      int
	maxValueByte int
	maxNameBytes int
	entries      map[string]Entry
	valueBytes   int
	nextRevision uint64
	generation   uint64
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

func validKeyByte(c byte) bool {
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
		if !validKeyByte(k[i]) {
			return false
		}
	}
	return true
}

func (s *Store) validValue(v []byte) bool {
	return v != nil && len(v) <= s.maxValueByte
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
		if c.Revision != 0 || !s.validValue(c.Value) {
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
		if !s.validValue(w.Value) {
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

func (s *Store) Transact(t Txn) (TxnResult, error) {
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
			pass = ok && bytes.Equal(e.Value, c.Value)
		}
		if !pass {
			return TxnResult{Succeeded: false, Generation: s.generation}, nil
		}
	}

	if len(t.Writes) == 0 {
		return TxnResult{Succeeded: true, Generation: s.generation}, nil
	}

	candidate := make(map[string]Entry, len(s.entries)+len(t.Writes))
	for k, e := range s.entries {
		candidate[k] = e
	}
	valueBytes := s.valueBytes
	nextRevision := s.nextRevision
	var lastRevision uint64

	for _, w := range t.Writes {
		switch w.Kind {
		case Put:
			if old, ok := candidate[w.Key]; ok {
				valueBytes -= len(old.Value)
			}
			v := make([]byte, len(w.Value))
			copy(v, w.Value)
			candidate[w.Key] = Entry{Key: w.Key, Revision: nextRevision, Value: v}
			valueBytes += len(v)
			lastRevision = nextRevision
			nextRevision++
		case Delete:
			old, ok := candidate[w.Key]
			if !ok {
				return TxnResult{}, ErrNotFound
			}
			valueBytes -= len(old.Value)
			delete(candidate, w.Key)
		}
	}

	if len(candidate) > s.maxKeys || valueBytes > s.maxValueByte {
		return TxnResult{}, ErrCapacity
	}

	s.entries = candidate
	s.valueBytes = valueBytes
	s.nextRevision = nextRevision
	s.generation++
	return TxnResult{Succeeded: true, Generation: s.generation, Revision: lastRevision}, nil
}

func (s *Store) Get(key string) (Entry, bool, error) {
	if !s.validKey(key) {
		return Entry{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[key]
	if !ok {
		return Entry{}, false, nil
	}
	v := make([]byte, len(e.Value))
	copy(v, e.Value)
	e.Value = v
	return e, true, nil
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		if k > after && (prefix == "" || (len(k) >= len(prefix) && k[:len(prefix)] == prefix)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		e := s.entries[k]
		v := make([]byte, len(e.Value))
		copy(v, e.Value)
		e.Value = v
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]Entry, 0, len(keys))
	for _, k := range keys {
		e := s.entries[k]
		v := make([]byte, len(e.Value))
		copy(v, e.Value)
		e.Value = v
		entries = append(entries, e)
	}
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Keys:         len(s.entries),
		ValueBytes:   s.valueBytes,
		Entries:      entries,
	}
}
