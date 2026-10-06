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

func checkedAdd(cur, delta int64) (int64, bool) {
	if delta > 0 && cur > math.MaxInt64-delta {
		return 0, false
	}
	if delta < 0 && cur < math.MinInt64-delta {
		return 0, false
	}
	return cur + delta, true
}

func withinAbs(v, max int64) bool {
	return v <= max && v >= -max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	var order []string
	touched := make(map[string]bool)
	touch := func(name string) {
		if !touched[name] {
			touched[name] = true
			order = append(order, name)
		}
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a := cand[op.Name]
			a.Name = op.Name
			nv, ok := checkedAdd(a.Value, op.Delta)
			if !ok || !withinAbs(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a.Value = nv
			a.Revision = rev
			rev++
			cand[op.Name] = a
			touch(op.Name)
		case Set:
			a := cand[op.Name]
			a.Name = op.Name
			a.Value = op.Value
			a.Revision = rev
			rev++
			cand[op.Name] = a
			touch(op.Name)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			touch(op.Name)
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = cand
	l.generation++
	l.nextRevision = rev
	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if a, ok := cand[name]; ok {
			changed = append(changed, a)
		}
	}
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
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
