// Package rewardledger implements a concurrency-safe in-memory reward
// points ledger with atomic batches, monotonic revisions and snapshots.
package rewardledger

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrValue          = errors.New("invalid value")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Add Kind = iota + 1
	Set
	Delete
)

type Options struct {
	MaxAccounts, MaxNameBytes int
	MaxAbsValue               int64
}
type Op struct {
	Kind         Kind
	Name         string
	Delta, Value int64
}
type Batch struct{ Ops []Op }
type Account struct {
	Name     string
	Value    int64
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Account
}
type Snapshot struct {
	Generation, NextRevision uint64
	Accounts                 []Account
}

// Ledger is a concurrency-safe in-memory account ledger.
// The zero value is not usable; construct it with New.
type Ledger struct {
	mu       sync.RWMutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64 // next revision to assign, starts at 1
}

// New validates opts and returns an empty ledger.
func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		opts:     opts,
		accounts: make(map[string]Account),
		nextRev:  1,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp performs structural validation of a single op without
// reading any ledger state.
func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add, Set, Delete:
	default:
		return ErrInvalidInput
	}
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	return nil
}

// checkValue enforces the absolute-value limit without overflowing on
// math.MinInt64 (the limit is positive, so -limit cannot underflow).
func (l *Ledger) checkValue(v int64) error {
	if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
		return ErrValue
	}
	return nil
}

// Apply executes the batch atomically in input order. The batch is fully
// validated structurally before any state is read; on any error the
// ledger is left untouched. Add/Set assign consecutive revisions; the
// generation increases exactly once per non-empty successful batch.
func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: clone the map so a failure anywhere leaves
	// the committed state untouched (full rollback).
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}

	nextRev := l.nextRev
	touched := make([]string, 0, len(b.Ops))
	seen := make(map[string]bool, len(b.Ops))

	for _, op := range b.Ops {
		if !seen[op.Name] {
			seen[op.Name] = true
			touched = append(touched, op.Name)
		}
		cur, exists := cand[op.Name]
		switch op.Kind {
		case Add:
			var nv int64
			if exists {
				// Detect int64 overflow before doing the arithmetic.
				if op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta ||
					op.Delta < 0 && cur.Value < math.MinInt64-op.Delta {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if err := l.checkValue(nv); err != nil {
				return Result{}, err
			}
			cand[op.Name] = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
		case Set:
			if err := l.checkValue(op.Value); err != nil {
				return Result{}, err
			}
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	// Account capacity is checked only against the final state.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(touched))
	for _, name := range touched {
		if acc, ok := cand[name]; ok {
			changed = append(changed, acc)
		}
	}

	l.accounts = cand
	l.nextRev = nextRev
	l.gen++
	return Result{Generation: l.gen, Revision: nextRev - 1, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, then name
// ascending. The returned slice is detached from internal state.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		all = append(all, acc)
	}
	l.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value > all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	if n > len(all) {
		n = len(all)
	}
	return all[:n], nil
}

// Snapshot returns a consistent view of the ledger with accounts sorted
// by name. The returned slice is detached from internal state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   l.gen,
		NextRevision: l.nextRev,
		Accounts:     accs,
	}
}
