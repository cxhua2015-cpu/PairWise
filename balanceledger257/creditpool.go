package balanceledger257

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

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func absOK(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	a := v
	if a < 0 {
		a = -a
	}
	return a <= limit
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
	next := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		next[k] = v
	}
	rev := l.nextRevision
	var changedOrder []string
	seen := make(map[string]bool)
	touch := func(name string) {
		if !seen[name] {
			seen[name] = true
			changedOrder = append(changedOrder, name)
		}
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a := next[op.Name]
			if (op.Delta > 0 && a.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && a.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			a.Value += op.Delta
			if !absOK(a.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a.Name = op.Name
			a.Revision = rev
			rev++
			next[op.Name] = a
			touch(op.Name)
		case Set:
			a := next[op.Name]
			a.Name = op.Name
			a.Value = op.Value
			a.Revision = rev
			rev++
			next[op.Name] = a
			touch(op.Name)
		case Delete:
			if _, ok := next[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
		}
	}
	if len(next) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	changed := make([]Account, 0, len(changedOrder))
	for _, name := range changedOrder {
		if a, ok := next[name]; ok {
			changed = append(changed, a)
		}
	}
	l.accounts = next
	l.nextRevision = rev
	l.generation++
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
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
