package resourceledger087

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
	mu       sync.RWMutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		opts:     o,
		accounts: make(map[string]Account),
		nextRev:  1,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func withinAbs(v, max int64) bool {
	return v <= max && v >= -max
}

func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

func (l *Ledger) validate(op Op) error {
	switch op.Kind {
	case Add:
		if op.Value != 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Delta != 0 {
			return ErrInvalidInput
		}
	case Delete:
		if op.Delta != 0 || op.Value != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	max := l.opts.MaxAbsValue
	switch op.Kind {
	case Add:
		if !withinAbs(op.Delta, max) {
			return ErrValue
		}
	case Set:
		if !withinAbs(op.Value, max) {
			return ErrValue
		}
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := l.validate(op); err != nil {
			return Result{}, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}

	nextRev := l.nextRev
	order := make([]string, 0, len(b.Ops))
	seen := make(map[string]bool, len(b.Ops))

	for _, op := range b.Ops {
		acc, ok := candidate[op.Name]
		switch op.Kind {
		case Add:
			value, okSum := addChecked(acc.Value, op.Delta)
			if !okSum || !withinAbs(value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Name = op.Name
			acc.Value = value
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
		case Set:
			acc.Name = op.Name
			acc.Value = op.Value
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if !seen[op.Name] {
			seen[op.Name] = true
			order = append(order, op.Name)
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.gen++
	}
	l.accounts = candidate
	l.nextRev = nextRev

	res := Result{Generation: l.gen, Revision: nextRev - 1}
	for _, name := range order {
		if acc, ok := candidate[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
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
	for _, acc := range l.accounts {
		accs = append(accs, acc)
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	snap := Snapshot{
		Generation:   l.gen,
		NextRevision: l.nextRev,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, acc := range l.accounts {
		snap.Accounts = append(snap.Accounts, acc)
	}
	sort.Slice(snap.Accounts, func(i, j int) bool {
		return snap.Accounts[i].Name < snap.Accounts[j].Name
	})
	return snap
}
