package balanceledger237

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

// Ledger is a concurrency-safe in-memory balance ledger. The accounts map is
// the sole authoritative index; every public method serializes through mu.
type Ledger struct {
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		opts:         o,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

// Apply executes the batch atomically in input order. It first runs the same
// structural validation as ValidateBatch, then replays the ops against a
// private candidate copy of the state; only a fully successful candidate is
// committed, so any failure leaves the ledger untouched.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}

	next := l.nextRevision
	var touched []string
	seen := make(map[string]bool, len(b.Ops))
	markTouched := func(name string) {
		if !seen[name] {
			seen[name] = true
			touched = append(touched, name)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc := candidate[op.Name]
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			v := acc.Value + op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			candidate[op.Name] = Account{Name: op.Name, Value: v, Revision: next}
			next++
			markTouched(op.Name)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			candidate[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: next}
			next++
			markTouched(op.Name)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = next
	if len(b.Ops) > 0 {
		l.generation++
	}

	res := Result{Generation: l.generation, Revision: next - 1}
	for _, name := range touched {
		if acc, ok := candidate[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
	}
	return res, nil
}

// Top returns up to n accounts ordered by value descending, then name
// ascending. The returned slice is independent of internal state.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		all = append(all, acc)
	}
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

// Snapshot returns a name-sorted deep copy of the current state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	for _, acc := range l.accounts {
		s.Accounts = append(s.Accounts, acc)
	}
	sort.Slice(s.Accounts, func(i, j int) bool {
		return s.Accounts[i].Name < s.Accounts[j].Name
	})
	return s
}
