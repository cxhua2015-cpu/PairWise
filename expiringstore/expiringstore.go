package expiringstore

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	MaxEntries, MaxTotalBytes, MaxValueBytes, MaxKeyBytes int
}

type OpKind uint8

const (
	Put OpKind = iota + 1
	Delete
	Touch
)

type Op struct {
	Kind      OpKind
	Key       string
	Value     []byte
	ExpiresAt int64
}

type Batch struct {
	Now int64
	Ops []Op
}

type Result struct {
	Generation, Revision uint64
	Expired              []string
}

type Entry struct {
	Key       string
	Value     []byte
	Revision  uint64
	ExpiresAt int64
}

type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	TotalBytes               int
	Entries                  []Entry
}

type entry struct {
	value     []byte
	revision  uint64
	expiresAt int64
}

type Store struct {
	mu         sync.Mutex
	maxEntries int
	maxTotal   int
	maxValue   int
	maxKey     int
	now        int64
	generation uint64
	nextRev    uint64
	totalBytes int
	entries    map[string]entry
}

func New(o Options) (*Store, error) {
	if o.MaxEntries <= 0 || o.MaxTotalBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if o.MaxValueBytes > o.MaxTotalBytes {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxEntries: o.MaxEntries,
		maxTotal:   o.MaxTotalBytes,
		maxValue:   o.MaxValueBytes,
		maxKey:     o.MaxKeyBytes,
		nextRev:    1,
		entries:    make(map[string]entry),
	}, nil
}

func (s *Store) validKey(k string) bool {
	if k == "" || len(k) > s.maxKey {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func (s *Store) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		if !s.validKey(op.Key) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if op.Value == nil || len(op.Value) > s.maxValue || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if op.Value != nil || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		case Touch:
			if op.Value != nil || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// expireFrom removes all entries expired at now and returns sorted keys.
func expireFrom(entries map[string]entry, now int64, totalBytes int) ([]string, int) {
	var expired []string
	for k, e := range entries {
		if e.expiresAt <= now {
			expired = append(expired, k)
			totalBytes -= len(e.value)
			delete(entries, k)
		}
	}
	sort.Strings(expired)
	return expired, totalBytes
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateBatch(b); err != nil {
		return Result{}, err
	}
	if b.Now < s.now {
		return Result{}, ErrTime
	}

	// Isolated candidate state.
	entries := make(map[string]entry, len(s.entries))
	for k, e := range s.entries {
		entries[k] = e
	}
	totalBytes := s.totalBytes
	nextRev := s.nextRev

	expired, totalBytes := expireFrom(entries, b.Now, totalBytes)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := entries[op.Key]; ok {
				totalBytes -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			entries[op.Key] = entry{value: v, revision: nextRev, expiresAt: op.ExpiresAt}
			totalBytes += len(v)
			nextRev++
		case Delete:
			old, ok := entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			totalBytes -= len(old.value)
			delete(entries, op.Key)
		case Touch:
			old, ok := entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			old.expiresAt = op.ExpiresAt
			entries[op.Key] = old
		}
	}

	if len(entries) > s.maxEntries || totalBytes > s.maxTotal {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.entries = entries
	s.totalBytes = totalBytes
	s.nextRev = nextRev
	s.now = b.Now
	if len(expired) > 0 || len(b.Ops) > 0 {
		s.generation++
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Expired: expired}, nil
}

func (s *Store) Get(key string, now int64) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validKey(key) {
		return Entry{}, false, ErrInvalidInput
	}
	if now < s.now {
		return Entry{}, false, ErrTime
	}
	expired, totalBytes := expireFrom(s.entries, now, s.totalBytes)
	s.totalBytes = totalBytes
	s.now = now
	if len(expired) > 0 {
		s.generation++
	}
	e, ok := s.entries[key]
	if !ok {
		return Entry{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Entry{Key: key, Value: v, Revision: e.revision, ExpiresAt: e.expiresAt}, true, nil
}

func (s *Store) Sweep(now int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now < s.now {
		return nil, ErrTime
	}
	expired, totalBytes := expireFrom(s.entries, now, s.totalBytes)
	s.totalBytes = totalBytes
	s.now = now
	if len(expired) > 0 {
		s.generation++
	}
	return expired, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]Entry, 0, len(keys))
	for _, k := range keys {
		e := s.entries[k]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		entries = append(entries, Entry{Key: k, Value: v, Revision: e.revision, ExpiresAt: e.expiresAt})
	}
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRev,
		Now:          s.now,
		TotalBytes:   s.totalBytes,
		Entries:      entries,
	}
}
