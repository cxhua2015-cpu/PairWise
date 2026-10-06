package balanceledger227

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
// State is kept in a hash index keyed by account name. Apply runs each
// batch as a candidate transaction against a private copy of the index and
// only commits (swaps the index and bumps the clocks) once every op and the
// final capacity check have succeeded, so failures roll back atomically.
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
	if err := validateBatch(l.opts, b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}
	// Candidate transaction: mutate a private copy so any failure is a no-op.
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	var order []string
	changed := make(map[string]Account)
	for _, op := range b.Ops {
		acc, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			nv, err := addChecked(acc.Value, op.Delta, ok, l.opts.MaxAbsValue)
			if err != nil {
				return Result{}, err
			}
			acc = Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			cand[op.Name] = acc
		case Set:
			acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(changed, op.Name)
			continue
		}
		if _, seen := changed[op.Name]; !seen {
			order = append(order, op.Name)
		}
		changed[op.Name] = acc
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = cand
	l.nextRevision = rev
	l.generation++
	res := Result{Generation: l.generation, Revision: rev - 1}
	for _, name := range order {
		if acc, ok := changed[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
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
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s.Accounts = append(s.Accounts, l.accounts[name])
	}
	return s
}
