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
	mu           sync.Mutex
	maxEntries   int
	maxTotal     int
	maxValue     int
	maxKey       int
	now          int64
	generation   uint64
	nextRevision uint64
	lastRevision uint64
	totalBytes   int
	entries      map[string]entry
}

func New(o Options) (*Store, error) {
	if o.MaxEntries <= 0 || o.MaxTotalBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if o.MaxValueBytes > o.MaxTotalBytes {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxEntries:   o.MaxEntries,
		maxTotal:     o.MaxTotalBytes,
		maxValue:     o.MaxValueBytes,
		maxKey:       o.MaxKeyBytes,
		nextRevision: 1,
		entries:      make(map[string]entry),
	}, nil
}

func validKey(key string, maxKey int) bool {
	if len(key) == 0 || len(key) > maxKey {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func (s *Store) validateOp(op Op, now int64) error {
	if !validKey(op.Key, s.maxKey) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Put:
		if op.Value == nil || len(op.Value) > s.maxValue || op.ExpiresAt <= now {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil || op.ExpiresAt != 0 {
			return ErrInvalidInput
		}
	case Touch:
		if op.Value != nil || op.ExpiresAt <= now {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// expireEntries removes every entry expired at now from entries, subtracts
// their value sizes from totalBytes, and returns the expired keys sorted.
func expireEntries(entries map[string]entry, now int64, totalBytes *int) []string {
	var expired []string
	for k, e := range entries {
		if e.expiresAt <= now {
			expired = append(expired, k)
		}
	}
	for _, k := range expired {
		*totalBytes -= len(entries[k].value)
		delete(entries, k)
	}
	sort.Strings(expired)
	return expired
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, op := range b.Ops {
		if err := s.validateOp(op, b.Now); err != nil {
			return Result{}, err
		}
	}
	if b.Now < s.now {
		return Result{}, ErrTime
	}

	// Isolated candidate state.
	cand := make(map[string]entry, len(s.entries))
	for k, e := range s.entries {
		cand[k] = e
	}
	totalBytes := s.totalBytes
	nextRevision := s.nextRevision
	var lastRevision uint64

	expired := expireEntries(cand, b.Now, &totalBytes)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := cand[op.Key]; ok {
				totalBytes -= len(old.value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			cand[op.Key] = entry{value: value, revision: nextRevision, expiresAt: op.ExpiresAt}
			totalBytes += len(value)
			lastRevision = nextRevision
			nextRevision++
		case Delete:
			old, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
			totalBytes -= len(old.value)
		case Touch:
			old, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			old.expiresAt = op.ExpiresAt
			cand[op.Key] = old
		}
	}

	if len(cand) > s.maxEntries || totalBytes > s.maxTotal {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.entries = cand
	s.totalBytes = totalBytes
	s.now = b.Now
	s.nextRevision = nextRevision
	if lastRevision != 0 {
		s.lastRevision = lastRevision
	}
	if len(expired) > 0 || len(b.Ops) > 0 {
		s.generation++
	}
	return Result{Generation: s.generation, Revision: s.lastRevision, Expired: expired}, nil
}

// advanceLocked expires due entries, advances time, and bumps the generation
// once if at least one entry expired.
func (s *Store) advanceLocked(now int64) []string {
	expired := expireEntries(s.entries, now, &s.totalBytes)
	if len(expired) > 0 {
		s.generation++
	}
	s.now = now
	return expired
}

func (s *Store) Get(key string, now int64) (Entry, bool, error) {
	if !validKey(key, s.maxKey) {
		return Entry{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now < s.now {
		return Entry{}, false, ErrTime
	}
	s.advanceLocked(now)
	e, ok := s.entries[key]
	if !ok {
		return Entry{}, false, nil
	}
	value := make([]byte, len(e.value))
	copy(value, e.value)
	return Entry{Key: key, Value: value, Revision: e.revision, ExpiresAt: e.expiresAt}, true, nil
}

func (s *Store) Sweep(now int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now < s.now {
		return nil, ErrTime
	}
	return s.advanceLocked(now), nil
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
		value := make([]byte, len(e.value))
		copy(value, e.value)
		entries = append(entries, Entry{Key: k, Value: value, Revision: e.revision, ExpiresAt: e.expiresAt})
	}
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Now:          s.now,
		TotalBytes:   s.totalBytes,
		Entries:      entries,
	}
}
