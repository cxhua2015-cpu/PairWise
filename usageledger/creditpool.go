package usageledger

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
	maxName      int
	maxAbs       int64
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

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		accounts:     make(map[string]Account),
		nextRevision: 1,
		maxAccounts:  o.MaxAccounts,
		maxName:      o.MaxNameBytes,
		maxAbs:       o.MaxAbsValue,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation, Revision: l.nextRevision - 1}
		l.mu.RUnlock()
		return r, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	mark := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			base := int64(0)
			if ok {
				base = cur.Value
			}
			if (op.Delta > 0 && base > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && base < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			v := base + op.Delta
			if v > l.maxAbs || v < -l.maxAbs {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			cand[op.Name] = a
			mark(a)
		case Set:
			if op.Value > l.maxAbs || op.Value < -l.maxAbs {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			mark(a)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(changedIdx, op.Name)
			for i, a := range changed {
				if a.Name == op.Name {
					changed = append(changed[:i], changed[i+1:]...)
					break
				}
			}
			for i, a := range changed {
				changedIdx[a.Name] = i
			}
		}
	}
	if len(cand) > l.maxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = cand
	l.nextRevision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	as := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		as = append(as, a)
	}
	l.mu.RUnlock()
	sort.Slice(as, func(i, j int) bool {
		if as[i].Value != as[j].Value {
			return as[i].Value > as[j].Value
		}
		return as[i].Name < as[j].Name
	})
	if n > len(as) {
		n = len(as)
	}
	return as[:n], nil
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
