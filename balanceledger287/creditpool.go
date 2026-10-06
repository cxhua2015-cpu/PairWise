package balanceledger287

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
	generation   uint64
	nextRevision uint64
	accounts     map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, nextRevision: 1, accounts: make(map[string]Account)}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	var changed []Account
	changedIdx := make(map[string]int)
	note := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a, ok := candidate[op.Name]
			if !ok {
				a = Account{Name: op.Name}
			}
			if (op.Delta > 0 && a.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && a.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			a.Value += op.Delta
			if a.Value > l.opts.MaxAbsValue || a.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a.Revision = nextRev
			nextRev++
			candidate[a.Name] = a
			note(a)
		case Set:
			a := candidate[op.Name]
			a.Name = op.Name
			a.Value = op.Value
			a.Revision = nextRev
			nextRev++
			candidate[a.Name] = a
			note(a)
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
	l.nextRevision = nextRev
	if len(b.Ops) > 0 {
		l.generation++
	}
	res := Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}
	if len(b.Ops) == 0 {
		res.Changed = nil
	}
	return res, nil
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
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
