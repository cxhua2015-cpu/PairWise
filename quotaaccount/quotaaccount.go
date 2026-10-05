// Package quotaaccount implements a concurrency-safe, in-memory quota
// account ledger with atomic batch application.
package quotaaccount

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
	mu         sync.Mutex
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
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
	// Phase 1: full structural validation before touching state.
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

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Phase 2: candidate transaction on a copy of the touched state.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	revision := l.revision
	changedIdx := make(map[string]int)
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := candidate[op.Name]
			if ok && op.Delta > 0 && acc.Value > l.opts.MaxAbsValue-op.Delta {
				return Result{}, ErrValue
			}
			if ok && op.Delta < 0 && acc.Value < -l.opts.MaxAbsValue-op.Delta {
				return Result{}, ErrValue
			}
			if !ok && (op.Delta > l.opts.MaxAbsValue || op.Delta < -l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			revision++
			acc.Value += op.Delta
			acc.Name = op.Name
			acc.Revision = revision
			candidate[op.Name] = acc
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			candidate[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: revision}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if op.Kind != Delete {
			if idx, seen := changedIdx[op.Name]; seen {
				changed[idx] = candidate[op.Name]
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, candidate[op.Name])
			}
		} else if idx, seen := changedIdx[op.Name]; seen {
			changed = append(changed[:idx], changed[idx+1:]...)
			delete(changedIdx, op.Name)
			for i := idx; i < len(changed); i++ {
				changedIdx[changed[i].Name] = i
			}
		}
	}

	// Capacity is checked only at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.revision = revision
	l.generation++
	return Result{Generation: l.generation, Revision: revision, Changed: changed}, nil
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
	return append([]Account(nil), accs[:n]...), nil
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
		NextRevision: l.revision + 1,
		Accounts:     accs,
	}
}
