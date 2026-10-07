package balanceledger407

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

// Ledger is a concurrency-safe in-memory balance ledger.
//
// State is kept in a single accounts map guarded by a RWMutex; Apply is a
// candidate-transaction: it validates, simulates on a private copy, and only
// commits (swapping the map and bumping the logical clocks) on full success.
type Ledger struct {
	mu      sync.RWMutex
	opts    Options
	accts   map[string]Account
	gen     uint64
	nextRev uint64
}

func New(o Options) (*Ledger, error) {
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	return &Ledger{opts: o, accts: make(map[string]Account), nextRev: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}
	// Candidate transaction: simulate on a private copy, commit atomically.
	cand := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		cand[k] = v
	}
	rev := l.nextRev
	var changed []Account
	pos := make(map[string]int, len(b.Ops))
	touch := func(a Account) {
		if i, ok := pos[a.Name]; ok {
			changed[i] = a
			return
		}
		pos[a.Name] = len(changed)
		changed = append(changed, a)
	}
	for _, op := range b.Ops {
		a, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta > 0 && a.Value > maxInt64-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && a.Value < minInt64-op.Delta {
				return Result{}, ErrValue
			}
			a.Value += op.Delta
			if a.Value > l.opts.MaxAbsValue || a.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a.Name = op.Name
			a.Revision = rev
			rev++
			cand[op.Name] = a
			touch(a)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			touch(a)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accts = cand
	l.gen++
	l.nextRev = rev
	return Result{Generation: l.gen, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value > all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	if n < len(all) {
		all = all[:n]
	}
	return all, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: make([]Account, 0, len(l.accts))}
	for _, a := range l.accts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}

const maxInt64 = int64(^uint64(0) >> 1)
const minInt64 = -maxInt64 - 1
