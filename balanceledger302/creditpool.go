package balanceledger302

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	revision     uint64
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
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

	candidate := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}

	revision := l.revision
	changedOrder := []string{}
	changed := map[string]Account{}
	recordChange := func(acc Account) {
		if _, ok := changed[acc.Name]; !ok {
			changedOrder = append(changedOrder, acc.Name)
		}
		changed[acc.Name] = acc
	}

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if !exists {
				acc = Account{Name: op.Name}
			}
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			value := acc.Value + op.Delta
			if value > l.maxAbsValue || value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			acc.Value = value
			acc.Revision = revision
			candidate[op.Name] = acc
			recordChange(acc)
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			candidate[op.Name] = acc
			recordChange(acc)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			delete(changed, op.Name)
		}
	}

	if len(candidate) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.generation++
	}
	l.revision = revision
	l.accounts = candidate

	result := Result{Generation: l.generation, Revision: l.revision}
	for _, name := range changedOrder {
		if acc, ok := changed[name]; ok {
			result.Changed = append(result.Changed, acc)
		}
	}
	return result, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	sorted := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		sorted = append(sorted, acc)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Value != sorted[j].Value {
			return sorted[i].Value > sorted[j].Value
		}
		return sorted[i].Name < sorted[j].Name
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	out := make([]Account, n)
	copy(out, sorted[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	snap := Snapshot{Generation: l.generation, NextRevision: l.revision + 1}
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		snap.Accounts = append(snap.Accounts, l.accounts[name])
	}
	return snap
}
