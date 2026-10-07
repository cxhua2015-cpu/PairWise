package balanceledger412

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

// Ledger is a concurrency-safe in-memory balance ledger.
// The zero value is not usable; construct it with New.
type Ledger struct {
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Ledger, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	return &Ledger{
		opts:         opts,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

// Apply executes the batch atomically in input order. On any failure the
// ledger is left untouched.
func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.validateBatchLocked(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: mutate a private copy, commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	nextRev := l.nextRevision
	var changed []Account
	seen := make(map[string]int)

	record := func(acc Account) {
		if idx, ok := seen[acc.Name]; ok {
			changed[idx] = acc
			return
		}
		seen[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := candidate[op.Name]
			if !ok {
				acc = Account{Name: op.Name}
			}
			v, err := addChecked(acc.Value, op.Delta, l.opts.MaxAbsValue)
			if err != nil {
				return Result{}, err
			}
			acc.Value = v
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			record(acc)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc := candidate[op.Name]
			acc.Name = op.Name
			acc.Value = op.Value
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			record(acc)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if idx, ok := seen[op.Name]; ok {
				changed = append(changed[:idx], changed[idx+1:]...)
				delete(seen, op.Name)
				for name, i := range seen {
					if i > idx {
						seen[name] = i - 1
					}
				}
			}
		}
	}

	// Final account capacity is only checked at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	l.generation++
	return Result{
		Generation: l.generation,
		Revision:   nextRev - 1,
		Changed:    changed,
	}, nil
}

// addChecked adds delta to cur, detecting int64 overflow before the
// arithmetic and enforcing the absolute-value limit on the result.
func addChecked(cur, delta, maxAbs int64) (int64, error) {
	if delta > 0 && cur > math.MaxInt64-delta {
		return 0, ErrValue
	}
	if delta < 0 && cur < math.MinInt64-delta {
		return 0, ErrValue
	}
	v := cur + delta
	if v > maxAbs || v < -maxAbs {
		return 0, ErrValue
	}
	return v, nil
}

// Top returns up to n accounts ordered by value descending, name ascending.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := l.sortedLocked(func(a, b Account) bool {
		if a.Value != b.Value {
			return a.Value > b.Value
		}
		return a.Name < b.Name
	})
	if n > len(all) {
		n = len(all)
	}
	return all[:n], nil
}

// Snapshot returns a name-ordered copy of the current state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts: l.sortedLocked(func(a, b Account) bool {
			return a.Name < b.Name
		}),
	}
}

// sortedLocked materializes accounts detached from internal state.
// Callers must hold at least the read lock.
func (l *Ledger) sortedLocked(less func(a, b Account) bool) []Account {
	out := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		out = append(out, acc)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
