// Package scoreboard implements a concurrency-safe, in-memory
// transactional scoreboard. See SPEC.md for the full contract.
package scoreboard

import (
	"errors"
	"math"
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
	mu      sync.RWMutex
	maxMem  int
	maxName int
	entries map[string]Entry
	gen     uint64
	nextRev uint64 // next revision to allocate; starts at 1
}

// New creates a Board. MaxMembers and MaxNameBytes must be positive.
func New(o Options) (*Board, error) {
	if o.MaxMembers <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Board{
		maxMem:  o.MaxMembers,
		maxName: o.MaxNameBytes,
		entries: make(map[string]Entry),
		nextRev: 1,
	}, nil
}

func (b *Board) validMember(m string) bool {
	if len(m) == 0 || len(m) > b.maxName {
		return false
	}
	for i := 0; i < len(m); i++ {
		c := m[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

// validateOp performs structural validation only; it never reads state.
func (b *Board) validateOp(op Op) error {
	if !b.validMember(op.Member) {
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

func lessEntry(a, b Entry) bool {
	if a.Score != b.Score {
		return a.Score > b.Score // score descending
	}
	return a.Member < b.Member // member ascending
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return lessEntry(es[i], es[j]) })
}

// Apply validates the whole batch, then executes it in input order on an
// isolated candidate. Any failure rolls back state, generation and
// revision allocation.
func (b *Board) Apply(batch Batch) (Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Phase 1: structural validation of every op, in input order.
	for _, op := range batch.Ops {
		if err := b.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	// Phase 2: execute on an isolated candidate copy.
	cand := make(map[string]Entry, len(b.entries)+len(batch.Ops))
	for k, v := range b.entries {
		cand[k] = v
	}
	nextRev := b.nextRev
	touched := make(map[string]struct{}, len(batch.Ops))

	for _, op := range batch.Ops {
		switch op.Kind {
		case Upsert:
			cand[op.Member] = Entry{Member: op.Member, Score: op.Score, Revision: nextRev}
			nextRev++
			touched[op.Member] = struct{}{}
		case Increment:
			e, ok := cand[op.Member]
			if !ok {
				return Result{}, ErrNotFound
			}
			if (op.Delta > 0 && e.Score > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && e.Score < math.MinInt64-op.Delta) {
				return Result{}, ErrOverflow
			}
			e.Score += op.Delta
			e.Revision = nextRev
			nextRev++
			cand[op.Member] = e
			touched[op.Member] = struct{}{}
		case Delete:
			if _, ok := cand[op.Member]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Member)
			touched[op.Member] = struct{}{}
		}
	}

	// Final capacity check only after all operations executed.
	if len(cand) > b.maxMem {
		return Result{}, ErrCapacity
	}

	// Commit.
	b.entries = cand
	res := Result{Revision: nextRev - 1}
	if len(batch.Ops) > 0 {
		b.gen++
	}
	res.Generation = b.gen
	b.nextRev = nextRev
	if len(batch.Ops) == 0 {
		res.Revision = 0
	}

	changed := make([]Entry, 0, len(touched))
	for m := range touched {
		if e, ok := cand[m]; ok {
			changed = append(changed, e)
		}
	}
	sortEntries(changed)
	res.Changed = changed
	return res, nil
}

// Get returns the entry for a valid member.
func (b *Board) Get(member string) (Entry, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.validMember(member) {
		return Entry{}, false, ErrInvalidInput
	}
	e, ok := b.entries[member]
	return e, ok, nil
}

// Range returns entries with scores in [min, max], ordered by score
// descending then member ascending, paginated by an optional cursor.
func (b *Board) Range(min, max int64, cursor Cursor, limit int) ([]Entry, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if min > max || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	if cursor.Set && !b.validMember(cursor.Member) {
		return nil, ErrInvalidInput
	}
	out := make([]Entry, 0, limit)
	for _, e := range b.entries {
		if e.Score < min || e.Score > max {
			continue
		}
		if cursor.Set && !lessEntry(cursorEntry(cursor), e) {
			continue // keep only entries strictly after the cursor
		}
		out = append(out, e)
	}
	sortEntries(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func cursorEntry(c Cursor) Entry { return Entry{Member: c.Member, Score: c.Score} }

// Snapshot returns an ownership-isolated, sorted copy of the board.
func (b *Board) Snapshot() Snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	es := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		es = append(es, e)
	}
	sortEntries(es)
	return Snapshot{Generation: b.gen, NextRevision: b.nextRev, Entries: es}
}
