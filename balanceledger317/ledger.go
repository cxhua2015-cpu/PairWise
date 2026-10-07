package balanceledger317

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
	mu           sync.Mutex
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
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
		acc     Account
		deleted bool
	}
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	changed := make(map[string]*change, len(b.Ops))
	var order []string
	nextRev := l.nextRevision

	mark := func(name string, acc Account, deleted bool) {
		if c, ok := changed[name]; ok {
			c.acc, c.deleted = acc, deleted
			return
		}
		changed[name] = &change{acc: acc, deleted: deleted}
		order = append(order, name)
	}

	for _, op := range b.Ops {
		acc, ok := candidate[op.Name]
		switch op.Kind {
		case Add:
			delta := op.Delta
			v := acc.Value
			if (delta > 0 && v > (1<<63-1)-delta) || (delta < 0 && v < (-1<<63)-delta) {
				return Result{}, ErrValue
			}
			v += delta
			if v > l.maxAbsValue || v < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: v, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
			mark(op.Name, acc, false)
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
			mark(op.Name, acc, false)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			mark(op.Name, Account{Name: op.Name}, true)
		}
	}

	if len(candidate) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	l.generation++
	res.Generation = l.generation
	res.Revision = nextRev - 1
	for _, name := range order {
		c := changed[name]
		if !c.deleted {
			res.Changed = append(res.Changed, c.acc)
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].Value != accs[j].Value {
			return accs[i].Value > accs[j].Value
		}
		return accs[i].Name < accs[j].Name
	})
	if n < len(accs) {
		accs = accs[:n]
	}
	return accs, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
