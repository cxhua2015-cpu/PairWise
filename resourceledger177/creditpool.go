package resourceledger177

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
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	staged := make(map[string]int, len(b.Ops))
	var order []string
	working := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		working[k] = v
	}
	revision := l.nextRevision

	for _, op := range b.Ops {
		acc, exists := working[op.Name]
		switch op.Kind {
		case Add:
			if !exists {
				acc = Account{Name: op.Name}
			}
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			acc.Value += op.Delta
			if acc.Value > l.opts.MaxAbsValue || acc.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			acc.Revision = revision
			working[op.Name] = acc
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			working[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: revision}
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(working, op.Name)
		}
		if _, ok := staged[op.Name]; !ok {
			staged[op.Name] = len(order)
			order = append(order, op.Name)
		}
	}

	if len(working) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = working
	l.generation++
	l.nextRevision = revision

	res := Result{Generation: l.generation, Revision: revision}
	for _, name := range order {
		if acc, ok := working[name]; ok {
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
	out := make([]Account, n)
	copy(out, accs[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: make([]Account, 0, len(names))}
	for _, name := range names {
		s.Accounts = append(s.Accounts, l.accounts[name])
	}
	return s
}
