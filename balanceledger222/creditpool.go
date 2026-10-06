package balanceledger222

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
	return &Ledger{
		opts:         o,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validate(b); err != nil {
		return Result{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	type touch struct {
		name    string
		deleted bool
	}
	work := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		work[k] = v
	}
	nextRev := l.nextRevision
	order := []touch{}
	touched := map[string]int{}

	mark := func(name string, deleted bool) {
		if i, ok := touched[name]; ok {
			order[i].deleted = deleted
			return
		}
		touched[name] = len(order)
		order = append(order, touch{name: name, deleted: deleted})
	}

	for _, op := range b.Ops {
		acc, exists := work[op.Name]
		switch op.Kind {
		case Add:
			v := acc.Value
			d := op.Delta
			if (d > 0 && v > math.MaxInt64-d) || (d < 0 && v < math.MinInt64-d) {
				return Result{}, ErrValue
			}
			nv := v + d
			if nv == math.MinInt64 || nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc.Name = op.Name
			acc.Value = nv
			acc.Revision = nextRev
			nextRev++
			work[op.Name] = acc
			mark(op.Name, false)
		case Set:
			acc.Name = op.Name
			acc.Value = op.Value
			acc.Revision = nextRev
			nextRev++
			work[op.Name] = acc
			mark(op.Name, false)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(work, op.Name)
			mark(op.Name, true)
		}
	}

	if len(work) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = work
	l.nextRevision = nextRev
	if len(b.Ops) > 0 {
		l.generation++
	}

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1}
	for _, t := range order {
		if t.deleted {
			continue
		}
		res.Changed = append(res.Changed, work[t.name])
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
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
