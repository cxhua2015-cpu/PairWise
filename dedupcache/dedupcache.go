package dedupcache

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
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxEntries, MaxKeyBytes, MaxTokenBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind       Kind
	Key, Token string
	Value      []byte
	ExpiresAt  int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Record struct {
	Key, Token string
	Value      []byte
	ExpiresAt  int64
	Revision   uint64
}
type Outcome struct {
	Key             string
	Created, Replay bool
	Record          Record
}
type Result struct {
	Generation, Revision uint64
	Outcomes             []Outcome
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Records                  []Record
}

type Cache struct {
	mu         sync.Mutex
	opts       Options
	now        int64
	generation uint64
	revision   uint64
	records    map[string]Record
	totalBytes int
}

func New(o Options) (*Cache, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 || o.MaxTokenBytes <= 0 ||
		o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 ||
		o.MaxValueBytes > o.MaxTotalValueBytes {
		return nil, ErrInvalidOptions
	}
	return &Cache{opts: o, records: make(map[string]Record)}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func (c *Cache) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validName(op.Key, c.opts.MaxKeyBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if !validName(op.Token, c.opts.MaxTokenBytes) ||
				op.Value == nil || len(op.Value) > c.opts.MaxValueBytes ||
				op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if op.Token != "" || op.Value != nil || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func cloneRecord(r Record) Record {
	r.Value = append([]byte(nil), r.Value...)
	return r
}

func (c *Cache) Apply(b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < c.now {
		return Result{}, ErrTime
	}

	records := make(map[string]Record, len(c.records))
	for k, r := range c.records {
		records[k] = r
	}
	total := c.totalBytes
	rev := c.revision
	changed := false

	for k, r := range records {
		if r.ExpiresAt <= b.Now {
			delete(records, k)
			total -= len(r.Value)
			changed = true
		}
	}

	outcomes := make([]Outcome, 0, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if r, ok := records[op.Key]; ok {
				if r.Token != op.Token {
					return Result{}, ErrConflict
				}
				outcomes = append(outcomes, Outcome{Key: op.Key, Replay: true, Record: cloneRecord(r)})
				continue
			}
			rev++
			r := Record{
				Key:       op.Key,
				Token:     op.Token,
				Value:     append([]byte(nil), op.Value...),
				ExpiresAt: op.ExpiresAt,
				Revision:  rev,
			}
			records[op.Key] = r
			total += len(r.Value)
			changed = true
			outcomes = append(outcomes, Outcome{Key: op.Key, Created: true, Record: cloneRecord(r)})
		case Delete:
			r, ok := records[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(records, op.Key)
			total -= len(r.Value)
			changed = true
			outcomes = append(outcomes, Outcome{Key: op.Key})
		}
	}

	if len(records) > c.opts.MaxEntries || total > c.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	c.records = records
	c.totalBytes = total
	c.revision = rev
	c.now = b.Now
	if changed {
		c.generation++
	}
	return Result{Generation: c.generation, Revision: c.revision, Outcomes: outcomes}, nil
}

func (c *Cache) Get(now int64, key string) (Record, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if now < 0 || !validName(key, c.opts.MaxKeyBytes) {
		return Record{}, false, ErrInvalidInput
	}
	if now < c.now {
		return Record{}, false, ErrTime
	}

	pruned := false
	for k, r := range c.records {
		if r.ExpiresAt <= now {
			delete(c.records, k)
			c.totalBytes -= len(r.Value)
			pruned = true
		}
	}
	if pruned {
		c.generation++
	}
	c.now = now

	r, ok := c.records[key]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(r), true, nil
}

func (c *Cache) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, len(c.records))
	for k := range c.records {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	recs := make([]Record, 0, len(keys))
	for _, k := range keys {
		recs = append(recs, cloneRecord(c.records[k]))
	}
	return Snapshot{
		Generation:   c.generation,
		NextRevision: c.revision + 1,
		Now:          c.now,
		Records:      recs,
	}
}
