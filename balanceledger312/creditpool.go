package balanceledger312

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
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		accounts:     make(map[string]Account),
		nextRevision: 1,
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// absOK reports whether |v| <= limit, safe for math.MinInt64.
func absOK(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
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
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation}
		l.mu.RUnlock()
		return r, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 2: candidate transaction on a cloned map.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	var changed []Account
	changedIdx := make(map[string]int)
	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	for _, op := range b.Ops {
		a, exists := cand[op.Name]
		switch op.Kind {
		case Add:
			if exists {
				v := a.Value + op.Delta
				if (op.Delta > 0 && v < a.Value) || (op.Delta < 0 && v > a.Value) {
					return Result{}, ErrValue
				}
				if !absOK(v, l.maxAbsValue) {
					return Result{}, ErrValue
				}
				a.Value = v
			} else {
				if !absOK(op.Delta, l.maxAbsValue) {
					return Result{}, ErrValue
				}
				a = Account{Name: op.Name, Value: op.Delta}
			}
			a.Revision = rev
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			if !absOK(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}
	// Phase 3: final capacity check at batch end.
	if len(cand) > l.maxAccounts {
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
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
	}
	l.mu.RUnlock()
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
