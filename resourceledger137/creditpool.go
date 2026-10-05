package resourceledger137

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

func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func absOK(v, maxAbs int64) bool {
	return v <= maxAbs && v >= -maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}
	// Candidate transaction: work on a copy, commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	var touched []string
	seen := make(map[string]bool)
	nextRev := l.nextRevision
	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if !absOK(op.Delta, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			// Overflow check before arithmetic.
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := acc.Value + op.Delta
			if !absOK(nv, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Value = nv
			acc.Revision = nextRev
			nextRev++
		case Set:
			if !absOK(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Value = op.Value
			acc.Revision = nextRev
			nextRev++
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if op.Kind != Delete {
			acc.Name = op.Name
			candidate[op.Name] = acc
		}
		if !seen[op.Name] {
			seen[op.Name] = true
			touched = append(touched, op.Name)
		}
	}
	if len(candidate) > l.maxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	changed := make([]Account, 0, len(touched))
	for _, name := range touched {
		if acc, ok := candidate[name]; ok {
			changed = append(changed, acc)
		}
	}
	return Result{Generation: l.generation, Revision: l.nextRevision - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
