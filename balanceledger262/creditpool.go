package balanceledger262

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
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage mutations on a private copy of the
	// touched entries so a failure anywhere rolls back for free.
	type staged struct {
		acc     Account
		exists  bool
		touched bool
	}
	stage := make(map[string]*staged, len(b.Ops))
	var order []string
	get := func(name string) *staged {
		if s, ok := stage[name]; ok {
			return s
		}
		s := &staged{}
		if a, ok := l.accounts[name]; ok {
			s.acc, s.exists = a, true
		}
		stage[name] = s
		order = append(order, name)
		return s
	}

	rev := l.nextRevision
	for _, op := range b.Ops {
		s := get(op.Name)
		switch op.Kind {
		case Add:
			if !s.exists {
				s.acc = Account{Name: op.Name}
				s.exists = true
			}
			v := s.acc.Value
			if (op.Delta > 0 && v > 0 && v > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && v < 0 && v < -(1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			v += op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			s.acc.Value = v
			s.acc.Revision = rev
			rev++
			s.touched = true
		case Set:
			if !s.exists {
				s.acc = Account{Name: op.Name}
				s.exists = true
			}
			s.acc.Value = op.Value
			s.acc.Revision = rev
			rev++
			s.touched = true
		case Delete:
			if !s.exists {
				return Result{}, ErrNotFound
			}
			s.exists = false
			s.acc = Account{Name: op.Name}
		}
	}

	count := len(l.accounts)
	for _, s := range stage {
		if s.exists {
			count++
		}
	}
	// subtract staged entries that already existed or were deleted
	for name, s := range stage {
		if _, ok := l.accounts[name]; ok {
			count--
		}
		if !s.exists {
			count--
		}
	}
	if count > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	res := Result{Generation: l.generation, Revision: rev - 1}
	for _, name := range order {
		s := stage[name]
		if s.exists {
			l.accounts[name] = s.acc
			if s.touched {
				res.Changed = append(res.Changed, s.acc)
			}
		} else {
			delete(l.accounts, name)
		}
	}
	l.nextRevision = rev
	if len(b.Ops) > 0 {
		l.generation++
		res.Generation = l.generation
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
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
	if n > len(accs) {
		n = len(accs)
	}
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
