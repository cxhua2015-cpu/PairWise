package balanceledger402

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

type account struct {
	value    int64
	revision uint64
}

type Ledger struct {
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]account), nextRevision: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage all mutations on a private copy so a
	// failure anywhere rolls the whole batch back for free.
	candidate := make(map[string]account, len(l.accounts))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	revision := l.nextRevision
	touched := make(map[string]bool, len(b.Ops))
	changed := make([]Account, 0, len(b.Ops))

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if (op.Delta > 0 && acc.value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			next := acc.value + op.Delta
			if next > l.opts.MaxAbsValue || next < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc.value = next
			acc.revision = revision
			revision++
			candidate[op.Name] = acc
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc.value = op.Value
			acc.revision = revision
			revision++
			candidate[op.Name] = acc
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if !touched[op.Name] {
			touched[op.Name] = true
			changed = append(changed, Account{Name: op.Name})
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit: non-empty successful batches advance the generation once.
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.accounts = candidate
	l.nextRevision = revision

	final := changed[:0]
	for _, entry := range changed {
		if acc, ok := candidate[entry.Name]; ok {
			final = append(final, Account{Name: entry.Name, Value: acc.value, Revision: acc.revision})
		}
	}
	return Result{Generation: l.generation, Revision: revision - 1, Changed: final}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := l.sortedAccounts(func(a, b Account) int {
		if a.Value != b.Value {
			if a.Value > b.Value {
				return -1
			}
			return 1
		}
		return compareName(a.Name, b.Name)
	})
	if n > len(all) {
		n = len(all)
	}
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts: l.sortedAccounts(func(a, b Account) int {
			return compareName(a.Name, b.Name)
		}),
	}
}

// sortedAccounts copies accounts out of the locked state and sorts them,
// so returned slices never alias internal state.
func (l *Ledger) sortedAccounts(cmp func(a, b Account) int) []Account {
	out := make([]Account, 0, len(l.accounts))
	for name, acc := range l.accounts {
		out = append(out, Account{Name: name, Value: acc.value, Revision: acc.revision})
	}
	sort.SliceStable(out, func(i, j int) bool { return cmp(out[i], out[j]) < 0 })
	return out
}

func compareName(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
