package balanceledger327

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
	maxAbs       int64
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
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbs:       o.MaxAbsValue,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
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

	type delta struct {
		acc   Account
		alive bool
	}
	pending := make(map[string]*delta)
	var order []string
	get := func(name string) *delta {
		if d, ok := pending[name]; ok {
			return d
		}
		acc, ok := l.accounts[name]
		d := &delta{acc: acc, alive: ok}
		pending[name] = d
		order = append(order, name)
		return d
	}

	revision := l.nextRevision
	for _, op := range b.Ops {
		d := get(op.Name)
		switch op.Kind {
		case Add:
			v := d.acc.Value
			if (op.Delta > 0 && v > 0 && v > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && v < 0 && v < -(1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := v + op.Delta
			if nv > l.maxAbs || nv < -l.maxAbs {
				return Result{}, ErrValue
			}
			d.acc = Account{Name: op.Name, Value: nv, Revision: revision}
			d.alive = true
			revision++
		case Set:
			if op.Value > l.maxAbs || op.Value < -l.maxAbs {
				return Result{}, ErrValue
			}
			d.acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			d.alive = true
			revision++
		case Delete:
			if !d.alive {
				return Result{}, ErrNotFound
			}
			d.alive = false
		}
	}

	// Final capacity check only, at end of batch.
	final := len(l.accounts)
	for name, d := range pending {
		_, exists := l.accounts[name]
		switch {
		case d.alive && !exists:
			final++
		case !d.alive && exists:
			final--
		}
	}
	if final > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		d := pending[name]
		if d.alive {
			l.accounts[name] = d.acc
			changed = append(changed, d.acc)
		} else {
			delete(l.accounts, name)
		}
	}
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = revision
	return Result{
		Generation: l.generation,
		Revision:   revision - 1,
		Changed:    changed,
	}, nil
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
	if n > len(accs) {
		n = len(accs)
	}
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
