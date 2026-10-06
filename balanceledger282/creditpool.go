package balanceledger282

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

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	touched := make(map[string]bool)
	var changed []Account

	record := func(a Account) {
		if touched[a.Name] {
			for i := range changed {
				if changed[i].Name == a.Name {
					changed[i] = a
					return
				}
			}
			return
		}
		touched[a.Name] = true
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		a, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			v := a.Value
			if (op.Delta > 0 && v > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && v < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			v += op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: v, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Set:
			a = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if touched[op.Name] {
				for i := range changed {
					if changed[i].Name == op.Name {
						changed = append(changed[:i], changed[i+1:]...)
						break
					}
				}
				delete(touched, op.Name)
			}
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	if len(b.Ops) > 0 {
		l.generation++
	}
	rev := uint64(0)
	if nextRev > 0 {
		rev = nextRev - 1
	}
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: all}
}
