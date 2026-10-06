package balanceledger242

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

// Ledger is a concurrency-safe in-memory balance ledger. The zero value is
// not usable; construct one with New.
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

// Apply executes the batch atomically in input order. On any error the
// candidate transaction is discarded and the ledger is left untouched.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: mutate a private copy, commit on success.
	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	nextRev := l.nextRevision
	touched := make([]string, 0, len(b.Ops))
	seen := make(map[string]bool, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := candidate[op.Name]
			if ok {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				acc.Value += op.Delta
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if acc.Value > l.opts.MaxAbsValue || acc.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
		case Set:
			acc := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if op.Kind != Delete && !seen[op.Name] {
			seen[op.Name] = true
			touched = append(touched, op.Name)
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(touched))
	for _, name := range touched {
		if acc, ok := candidate[name]; ok {
			changed = append(changed, acc)
		}
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	l.generation++
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, name ascending.
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
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
}

// Snapshot returns a name-sorted copy of the current state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
