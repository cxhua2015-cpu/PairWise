package capacityledger

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
	mu         sync.RWMutex
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64 // last assigned revision; NextRevision = revision + 1
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
}

func validName(o Options, n string) bool {
	if n == "" || len(n) > o.MaxNameBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func withinAbs(o Options, v int64) bool {
	return v <= o.MaxAbsValue && v >= -o.MaxAbsValue
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(l.opts, op.Name) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 2: execute against a candidate copy; commit only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	revision := l.revision
	var order []string
	touched := make(map[string]bool, len(b.Ops))
	touch := func(n string) {
		if !touched[n] {
			touched[n] = true
			order = append(order, n)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := cand[op.Name]
			var nv int64
			if ok {
				// Detect int64 overflow before performing arithmetic.
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = acc.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if !withinAbs(l.opts, nv) {
				return Result{}, ErrValue
			}
			revision++
			cand[op.Name] = Account{Name: op.Name, Value: nv, Revision: revision}
			touch(op.Name)
		case Set:
			if !withinAbs(l.opts, op.Value) {
				return Result{}, ErrValue
			}
			revision++
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: revision}
			touch(op.Name)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			touch(op.Name)
		}
	}

	// Final account-count capacity check at end of batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = cand
	l.revision = revision
	if len(b.Ops) > 0 {
		l.generation++
	}
	changed := make([]Account, 0, len(order))
	for _, n := range order {
		if acc, ok := cand[n]; ok {
			changed = append(changed, acc)
		}
	}
	return Result{Generation: l.generation, Revision: l.revision, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.revision + 1, Accounts: all}
}
