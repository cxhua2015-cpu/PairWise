package budgetledger

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func absOK(v, maxAbs int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= maxAbs
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1}
	if len(b.Ops) == 0 {
		return res, nil
	}

	type change struct {
		name    string
		account Account
		exists  bool
	}
	pending := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		pending[k] = v
	}
	var order []string
	touched := make(map[string]bool)
	revision := l.nextRevision

	mark := func(name string) {
		if !touched[name] {
			touched[name] = true
			order = append(order, name)
		}
	}

	for _, op := range b.Ops {
		cur, exists := pending[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta == math.MinInt64 {
				return Result{}, ErrValue
			}
			if op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && cur.Value < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			nv := cur.Value + op.Delta
			if !absOK(nv, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			pending[op.Name] = Account{Name: op.Name, Value: nv, Revision: revision}
			revision++
			mark(op.Name)
		case Set:
			if !absOK(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			pending[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: revision}
			revision++
			mark(op.Name)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(pending, op.Name)
			mark(op.Name)
		}
	}

	if len(pending) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = pending
	l.generation++
	l.nextRevision = revision
	res.Generation = l.generation
	res.Revision = revision - 1
	for _, name := range order {
		if a, ok := pending[name]; ok {
			res.Changed = append(res.Changed, a)
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if n <= 0 {
		return nil, nil
	}
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
