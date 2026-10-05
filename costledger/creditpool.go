package costledger

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
	mu         sync.RWMutex
	maxAccts   int
	maxName    int
	maxAbs     int64
	accounts   map[string]Account
	generation uint64
	nextRev    uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccts: o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		accounts: make(map[string]Account),
		nextRev:  1,
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

// validateOp performs structural validation only; it must not read state.
func (l *Ledger) validateOp(op Op) error {
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
	if !validName(op.Name, l.maxName) {
		return ErrInvalidInput
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if b.Ops == nil {
		b.Ops = nil
	}
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}

	// Phase 2: execute against a candidate copy; commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRev
	var changed []Account
	changedIdx := make(map[string]int)

	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			base := int64(0)
			if exists {
				base = cur.Value
			}
			if (op.Delta > 0 && base > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && base < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if nv > l.maxAbs || nv < -l.maxAbs {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Set:
			if op.Value > l.maxAbs || op.Value < -l.maxAbs {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			record(Account{Name: op.Name, Value: 0, Revision: cur.Revision})
		}
	}

	if len(candidate) > l.maxAccts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRev = nextRev
	l.generation++
	return Result{
		Generation: l.generation,
		Revision:   nextRev - 1,
		Changed:    changed,
	}, nil
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
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRev,
		Accounts:     accts,
	}
}
