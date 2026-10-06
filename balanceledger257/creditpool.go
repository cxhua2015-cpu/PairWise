package balanceledger257

import (
	"errors"
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
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRevision: 1}, nil
}

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: stage mutations on copies; commit only on success.
	staged := make(map[string]Account)
	deleted := make(map[string]bool)
	live := func(name string) (Account, bool) {
		if deleted[name] {
			return Account{}, false
		}
		if a, ok := staged[name]; ok {
			return a, true
		}
		a, ok := l.accounts[name]
		return a, ok
	}
	rev := l.nextRevision
	var order []string
	seen := make(map[string]bool)

	for _, op := range b.Ops {
		a, exists := live(op.Name)
		switch op.Kind {
		case Add:
			cur := a.Value
			if (op.Delta > 0 && cur > maxInt64-op.Delta) ||
				(op.Delta < 0 && cur < minInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := cur + op.Delta
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			delete(deleted, op.Name)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			delete(deleted, op.Name)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			deleted[op.Name] = true
			a = Account{}
		}
		staged[op.Name] = a
		if !seen[op.Name] {
			seen[op.Name] = true
			order = append(order, op.Name)
		}
	}

	finalCount := len(l.accounts)
	for name := range staged {
		_, committed := l.accounts[name]
		if deleted[name] {
			if committed {
				finalCount--
			}
		} else if !committed {
			finalCount++
		}
	}
	if finalCount > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit the candidate transaction.
	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if deleted[name] {
			delete(l.accounts, name)
			continue
		}
		a := staged[name]
		l.accounts[name] = a
		changed = append(changed, a)
	}
	l.generation++
	l.nextRevision = rev
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
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
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     l.sortedAccounts(),
	}
}

// sortedAccounts returns accounts ordered by name; caller must hold the lock.
func (l *Ledger) sortedAccounts() []Account {
	out := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
