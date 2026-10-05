package resourceledger147

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

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, accounts: make(map[string]Account), nextRevision: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
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

	limit := l.opts.MaxAbsValue
	candidate := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	revision := l.nextRevision
	changed := make([]Account, 0, len(b.Ops))
	changedIdx := make(map[string]int)

	note := func(acc Account) {
		if i, ok := changedIdx[acc.Name]; ok {
			changed[i] = acc
			return
		}
		changedIdx[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			var v int64
			if exists {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				v = acc.Value + op.Delta
			} else {
				v = op.Delta
			}
			if v > limit || v < -limit {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: v, Revision: revision}
			revision++
			candidate[op.Name] = acc
			note(acc)
		case Set:
			if op.Value > limit || op.Value < -limit {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			revision++
			candidate[op.Name] = acc
			note(acc)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.generation++
	}
	l.accounts = candidate
	l.nextRevision = revision
	return Result{Generation: l.generation, Revision: revision - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	l.mu.RUnlock()
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
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
