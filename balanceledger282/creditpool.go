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

func absOK(v, max int64) bool {
	if v == math.MinInt64 {
		return false
	}
	a := v
	if a < 0 {
		a = -a
	}
	return a <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation}
		l.mu.RUnlock()
		return r, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	var changed []Account
	seen := make(map[string]int)

	record := func(a Account) {
		if i, ok := seen[a.Name]; ok {
			changed[i] = a
		} else {
			seen[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		a, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			v := op.Delta
			if ok {
				if (op.Delta > 0 && a.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && a.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				v = a.Value + op.Delta
			}
			if !absOK(v, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok2 := seen[op.Name]; ok2 {
				changed = append(changed[:i], changed[i+1:]...)
				delete(seen, op.Name)
				for j := i; j < len(changed); j++ {
					seen[changed[j].Name] = j
				}
			}
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = cand
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
