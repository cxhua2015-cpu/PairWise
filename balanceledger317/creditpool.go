package balanceledger317

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
	mu         sync.Mutex
	maxAcct    int
	maxName    int
	maxAbs     int64
	generation uint64
	revision   uint64
	accounts   map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAcct:  o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		accounts: make(map[string]Account),
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

func (l *Ledger) checkAbs(v int64) error {
	if v == math.MinInt64 || v > l.maxAbs || v < -l.maxAbs {
		return ErrValue
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Phase 2: execute as a candidate transaction; undo log restores on failure.
	type undoEntry struct {
		name    string
		prev    Account
		existed bool
	}
	var undo []undoEntry
	touched := make(map[string]bool)
	changedIdx := make(map[string]int)
	var changed []Account
	oldRevision := l.revision

	fail := func(err error) (Result, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			u := undo[i]
			if u.existed {
				l.accounts[u.name] = u.prev
			} else {
				delete(l.accounts, u.name)
			}
		}
		l.revision = oldRevision
		return Result{}, err
	}

	record := func(name string) {
		if !touched[name] {
			touched[name] = true
			prev, ok := l.accounts[name]
			undo = append(undo, undoEntry{name, prev, ok})
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			record(op.Name)
			acct := l.accounts[op.Name]
			// Detect int64 overflow before arithmetic.
			if (op.Delta > 0 && acct.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acct.Value < math.MinInt64-op.Delta) {
				return fail(ErrValue)
			}
			nv := acct.Value + op.Delta
			if err := l.checkAbs(nv); err != nil {
				return fail(err)
			}
			l.revision++
			acct.Value = nv
			acct.Revision = l.revision
			acct.Name = op.Name
			l.accounts[op.Name] = acct
			if i, ok := changedIdx[op.Name]; ok {
				changed[i] = acct
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, acct)
			}
		case Set:
			if err := l.checkAbs(op.Value); err != nil {
				return fail(err)
			}
			record(op.Name)
			l.revision++
			acct := Account{Name: op.Name, Value: op.Value, Revision: l.revision}
			l.accounts[op.Name] = acct
			if i, ok := changedIdx[op.Name]; ok {
				changed[i] = acct
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, acct)
			}
		case Delete:
			if _, ok := l.accounts[op.Name]; !ok {
				return fail(ErrNotFound)
			}
			record(op.Name)
			delete(l.accounts, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				delete(changedIdx, op.Name)
				changed = append(changed[:i], changed[i+1:]...)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	// Final capacity check only at batch end.
	if len(l.accounts) > l.maxAcct {
		return fail(ErrCapacity)
	}

	l.generation++
	return Result{Generation: l.generation, Revision: l.revision, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	accts := make([]Account, len(names))
	for i, name := range names {
		accts[i] = l.accounts[name]
	}
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accts,
	}
}
