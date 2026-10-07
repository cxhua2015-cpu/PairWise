package balanceledger342

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
type Ledger struct {
	mu       sync.Mutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: map[string]Account{}, nextRev: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}

	next := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		next[k] = v
	}
	rev := l.nextRev
	var changed []Account
	touched := map[string]int{}

	record := func(a Account) {
		if i, ok := touched[a.Name]; ok {
			changed[i] = a
		} else {
			touched[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		cur, ok := next[op.Name]
		switch op.Kind {
		case Add:
			v := cur.Value
			d := op.Delta
			if (d > 0 && v > 0 && v > (1<<63-1)-d) ||
				(d < 0 && v < 0 && v < -(1<<63)-d) {
				return Result{}, ErrValue
			}
			v += d
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			next[op.Name] = cur
			record(cur)
		case Set:
			v := op.Value
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			next[op.Name] = cur
			record(cur)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
			if i, ok2 := touched[op.Name]; ok2 {
				changed = append(changed[:i], changed[i+1:]...)
				delete(touched, op.Name)
				for n, idx := range touched {
					if idx > i {
						touched[n] = idx - 1
					}
				}
			}
		}
	}

	if len(next) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = next
	l.gen++
	l.nextRev = rev
	return Result{Generation: l.gen, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool {
		return s.Accounts[i].Name < s.Accounts[j].Name
	})
	return s
}
