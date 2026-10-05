package resourceledger187

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
	mu           sync.Mutex
	opts         Options
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

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRevision: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if b.Ops == nil {
		b.Ops = []Op{}
	}
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1}
	if len(b.Ops) == 0 {
		return res, nil
	}

	// Candidate transaction: stage changes on a copy; commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	touched := make(map[string]bool)
	var order []string

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			var nv int64
			if exists {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = acc.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if !touched[op.Name] {
			touched[op.Name] = true
			order = append(order, op.Name)
		}
	}

	// Final account capacity is checked only at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	res.Generation = l.generation
	res.Revision = nextRev - 1
	for _, name := range order {
		if acc, ok := candidate[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		s.Accounts = append(s.Accounts, acc)
	}
	sort.Slice(s.Accounts, func(i, j int) bool {
		return s.Accounts[i].Name < s.Accounts[j].Name
	})
	return s
}
