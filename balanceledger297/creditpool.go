package balanceledger297

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
	mu       sync.RWMutex
	opts     Options
	accounts map[string]*account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	if o.MaxAbsValue > math.MaxInt64-1 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		opts:     o,
		accounts: make(map[string]*account),
		nextRev:  1,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		res := Result{Generation: l.gen, Revision: l.nextRev - 1}
		l.mu.RUnlock()
		return res, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]*account, len(l.accounts)+len(b.Ops))
	for n, a := range l.accounts {
		cp := *a
		cand[n] = &cp
	}
	order := make([]string, 0, len(b.Ops))
	seen := make(map[string]bool, len(b.Ops))
	lastRev := l.nextRev

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur := int64(0)
			if a, ok := cand[op.Name]; ok {
				cur = a.value
			}
			nv, err := addClamped(cur, op.Delta, l.opts.MaxAbsValue)
			if err != nil {
				return Result{}, err
			}
			a, ok := cand[op.Name]
			if !ok {
				a = &account{}
				cand[op.Name] = a
			}
			a.value = nv
			a.revision = lastRev
			lastRev++
		case Set:
			if op.Value < -l.opts.MaxAbsValue || op.Value > l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a, ok := cand[op.Name]
			if !ok {
				a = &account{}
				cand[op.Name] = a
			}
			a.value = op.Value
			a.revision = lastRev
			lastRev++
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
		if !seen[op.Name] {
			seen[op.Name] = true
			order = append(order, op.Name)
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRev = lastRev
	l.gen++

	changed := make([]Account, 0, len(order))
	for _, n := range order {
		if a, ok := cand[n]; ok {
			changed = append(changed, Account{Name: n, Value: a.value, Revision: a.revision})
		}
	}
	return Result{Generation: l.gen, Revision: l.nextRev - 1, Changed: changed}, nil
}

func addClamped(cur, delta, cap int64) (int64, error) {
	if delta > 0 && cur > math.MaxInt64-delta {
		return 0, ErrValue
	}
	if delta < 0 && cur < math.MinInt64-delta {
		return 0, ErrValue
	}
	nv := cur + delta
	if nv < -cap || nv > cap {
		return 0, ErrValue
	}
	return nv, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accounts))
	for name, a := range l.accounts {
		all = append(all, Account{Name: name, Value: a.value, Revision: a.revision})
	}
	l.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value > all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	if n > 0 && n < len(all) {
		all = all[:n]
	}
	return all, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	snap := Snapshot{Generation: l.gen, NextRevision: l.nextRev}
	snap.Accounts = make([]Account, 0, len(l.accounts))
	for name, a := range l.accounts {
		snap.Accounts = append(snap.Accounts, Account{Name: name, Value: a.value, Revision: a.revision})
	}
	l.mu.RUnlock()
	sort.Slice(snap.Accounts, func(i, j int) bool {
		return snap.Accounts[i].Name < snap.Accounts[j].Name
	})
	return snap
}
