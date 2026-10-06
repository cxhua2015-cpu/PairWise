package balanceledger247

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
	mu         sync.RWMutex
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage all mutations on a copy of the touched
	// accounts so a failure anywhere rolls back the whole batch.
	type staged struct {
		acc     Account
		exists  bool
		deleted bool
	}
	stage := make(map[string]*staged, len(b.Ops))
	order := make([]string, 0, len(b.Ops))
	get := func(name string) (Account, bool) {
		if s, ok := stage[name]; ok && (s.exists || s.deleted) {
			return s.acc, s.exists && !s.deleted
		}
		a, ok := l.accounts[name]
		return a, ok
	}
	revision := l.revision
	changed := make(map[string]Account, len(b.Ops))

	for _, op := range b.Ops {
		s, ok := stage[op.Name]
		if !ok {
			s = &staged{}
			stage[op.Name] = s
			order = append(order, op.Name)
		}
		cur, exists := get(op.Name)
		switch op.Kind {
		case Add:
			if op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && cur.Value < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			next := cur.Value + op.Delta
			if next > l.opts.MaxAbsValue || next < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			if !exists {
				cur = Account{Name: op.Name}
			}
			cur.Value = next
			revision++
			cur.Revision = revision
			s.acc, s.exists, s.deleted = cur, true, false
			changed[op.Name] = cur
		case Set:
			if !exists {
				cur = Account{Name: op.Name}
			}
			cur.Value = op.Value
			revision++
			cur.Revision = revision
			s.acc, s.exists, s.deleted = cur, true, false
			changed[op.Name] = cur
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			s.deleted = true
			delete(changed, op.Name)
		}
	}

	// Final account capacity is checked only at the end of the batch.
	finalCount := len(l.accounts)
	for _, name := range order {
		s := stage[name]
		_, committed := l.accounts[name]
		switch {
		case s.deleted && committed:
			finalCount--
		case !s.deleted && s.exists && !committed:
			finalCount++
		}
	}
	if finalCount > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	for _, name := range order {
		s := stage[name]
		if s.deleted {
			delete(l.accounts, name)
		} else if s.exists {
			l.accounts[name] = s.acc
		}
	}
	l.revision = revision
	if len(b.Ops) > 0 {
		l.generation++
	}
	out := Result{Generation: l.generation, Revision: l.revision}
	for _, name := range order {
		if acc, ok := changed[name]; ok {
			out.Changed = append(out.Changed, acc)
		}
	}
	return out, nil
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
	s := Snapshot{Generation: l.generation, NextRevision: l.revision + 1}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
