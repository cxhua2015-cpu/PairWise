package expirytable309

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Touch
	Delete
)

type Options struct{ MaxEntries, MaxKeyBytes int }
type Op struct {
	Kind      Kind
	Key       string
	ExpiresAt int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Entry struct {
	Key       string
	ExpiresAt int64
	Revision  uint64
}
type Result struct{ Generation, Revision uint64 }
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  []Entry
}

type Table struct {
	mu     sync.Mutex
	max    int
	maxKey int
	now    int64
	gen    uint64
	rev    uint64
	items  map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{max: o.MaxEntries, maxKey: o.MaxKeyBytes, items: map[string]Entry{}}, nil
}

func validKey(k string, max int) bool {
	if len(k) == 0 || len(k) > max {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (t *Table) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Touch && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKey) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: t.gen, Revision: t.rev}, nil
	}
	items := make(map[string]Entry, len(t.items))
	for k, e := range t.items {
		if e.ExpiresAt > b.Now {
			items[k] = e
		}
	}
	rev := t.rev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			items[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
		case Touch:
			_, ok := items[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			items[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
		case Delete:
			if _, ok := items[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.Key)
		}
	}
	if len(items) > t.max {
		return Result{}, ErrCapacity
	}
	t.items = items
	t.now = b.Now
	t.rev = rev
	t.gen++
	return Result{Generation: t.gen, Revision: rev}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 {
		return nil, ErrInvalidInput
	}
	if now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	for k, e := range t.items {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.items, k)
		}
	}
	sort.Slice(gone, func(i, j int) bool {
		if gone[i].ExpiresAt != gone[j].ExpiresAt {
			return gone[i].ExpiresAt < gone[j].ExpiresAt
		}
		return gone[i].Key < gone[j].Key
	})
	t.now = now
	if len(gone) > 0 {
		t.gen++
	}
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{Generation: t.gen, NextRevision: t.rev + 1, Now: t.now}
	for _, e := range t.items {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
