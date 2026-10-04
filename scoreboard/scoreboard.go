// Package scoreboard implements a concurrency-safe in-memory
// transactional scoreboard. See SPEC.md for the full contract.
package scoreboard

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrOverflow       = errors.New("score overflow")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxMembers, MaxNameBytes int }

type OpKind uint8

const (
	Upsert OpKind = iota + 1
	Increment
	Delete
)

type Op struct {
	Kind         OpKind
	Member       string
	Score, Delta int64
}

type Batch struct{ Ops []Op }

type Entry struct {
	Member   string
	Score    int64
	Revision uint64
}

type Result struct {
	Generation, Revision uint64
	Changed              []Entry
}

type Cursor struct {
	Set    bool
	Score  int64
	Member string
}

type Snapshot struct {
	Generation, NextRevision uint64
	Entries                  []Entry
}

type Board struct {
	mu         sync.RWMutex
	maxMembers int
	maxName    int
	members    map[string]Entry
	generation uint64
	nextRev    uint64
}

func New(o Options) (*Board, error) {
	if o.MaxMembers <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Board{
		maxMembers: o.MaxMembers,
		maxName:    o.MaxNameBytes,
		members:    make(map[string]Entry),
		nextRev:    1,
	}, nil
}

func validName(name string, maxName int) bool {
	if len(name) == 0 || len(name) > maxName {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func (b *Board) validateOp(op Op) error {
	if !validName(op.Member, b.maxName) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Upsert:
		if op.Delta != 0 {
			return ErrInvalidInput
		}
	case Increment:
		if op.Score != 0 || op.Delta == 0 {
			return ErrInvalidInput
		}
	case Delete:
		if op.Score != 0 || op.Delta != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func addOverflows(s, d int64) bool {
	return (d > 0 && s > (1<<63-1)-d) || (d < 0 && s < (-1<<63)-d)
}

// less orders entries by score descending, then member ascending.
func less(a, b Entry) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.Member < b.Member
}

func (b *Board) Apply(batch Batch) (Result, error) {
	for _, op := range batch.Ops {
		if err := b.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	candidate := make(map[string]Entry, len(b.members)+len(batch.Ops))
	for k, v := range b.members {
		candidate[k] = v
	}
	nextRev := b.nextRev
	touched := make(map[string]struct{}, len(batch.Ops))

	for _, op := range batch.Ops {
		touched[op.Member] = struct{}{}
		switch op.Kind {
		case Upsert:
			candidate[op.Member] = Entry{Member: op.Member, Score: op.Score, Revision: nextRev}
			nextRev++
		case Increment:
			e, ok := candidate[op.Member]
			if !ok {
				return Result{}, ErrNotFound
			}
			if addOverflows(e.Score, op.Delta) {
				return Result{}, ErrOverflow
			}
			e.Score += op.Delta
			e.Revision = nextRev
			candidate[op.Member] = e
			nextRev++
		case Delete:
			if _, ok := candidate[op.Member]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Member)
		}
	}

	if len(candidate) > b.maxMembers {
		return Result{}, ErrCapacity
	}

	b.members = candidate
	b.nextRev = nextRev
	if len(batch.Ops) > 0 {
		b.generation++
	}

	changed := make([]Entry, 0, len(touched))
	for m := range touched {
		if e, ok := candidate[m]; ok {
			changed = append(changed, e)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return less(changed[i], changed[j]) })

	return Result{Generation: b.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (b *Board) Get(member string) (Entry, bool, error) {
	if !validName(member, b.maxName) {
		return Entry{}, false, ErrInvalidInput
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	e, ok := b.members[member]
	return e, ok, nil
}

func (b *Board) Range(min, max int64, cursor Cursor, limit int) ([]Entry, error) {
	if min > max || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	if cursor.Set && !validName(cursor.Member, b.maxName) {
		return nil, ErrInvalidInput
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	entries := make([]Entry, 0, len(b.members))
	for _, e := range b.members {
		if e.Score >= min && e.Score <= max {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return less(entries[i], entries[j]) })

	out := make([]Entry, 0, limit)
	for _, e := range entries {
		if cursor.Set && !less(Entry{Member: cursor.Member, Score: cursor.Score}, e) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (b *Board) Snapshot() Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	entries := make([]Entry, 0, len(b.members))
	for _, e := range b.members {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return less(entries[i], entries[j]) })
	return Snapshot{Generation: b.generation, NextRevision: b.nextRev, Entries: entries}
}
