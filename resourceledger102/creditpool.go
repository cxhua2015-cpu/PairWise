package resourceledger102

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
	maxAccts   int
	maxName    int
	maxAbs     int64
	accounts   map[string]Account
	generation uint64
	revision   uint64
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
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

	// Phase 2: candidate transaction. Staged writes overlay the base map;
	// nothing is committed until every op and the final capacity check pass.
	type staged struct {
		acct    Account
		deleted bool
	}
	cand := make(map[string]*staged)
	changed := make([]Account, 0, len(b.Ops))
	changedIdx := make(map[string]int)
	revision := l.revision

	lookup := func(name string) (Account, bool) {
		if s, ok := cand[name]; ok {
			return s.acct, !s.deleted
		}
		a, ok := l.accounts[name]
		return a, ok
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Delta > l.maxAbs || op.Delta < -l.maxAbs {
				return Result{}, ErrValue
			}
			cur, ok := lookup(op.Name)
			var base int64
			if ok {
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
			revision++
			cand[op.Name] = &staged{acct: Account{Name: op.Name, Value: nv, Revision: revision}}
		case Set:
			if op.Value > l.maxAbs || op.Value < -l.maxAbs {
				return Result{}, ErrValue
			}
			revision++
			cand[op.Name] = &staged{acct: Account{Name: op.Name, Value: op.Value, Revision: revision}}
		case Delete:
			if _, ok := lookup(op.Name); !ok {
				return Result{}, ErrNotFound
			}
			cand[op.Name] = &staged{deleted: true}
		}
		// Track Changed: one entry per touched account, first-touch order.
		if s := cand[op.Name]; s.deleted {
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		} else if i, seen := changedIdx[op.Name]; seen {
			changed[i] = s.acct
		} else {
			changedIdx[op.Name] = len(changed)
			changed = append(changed, s.acct)
		}
	}

	// Final account capacity is checked only at the end of the batch.
	finalCount := len(l.accounts)
	for name, s := range cand {
		_, exists := l.accounts[name]
		switch {
		case s.deleted && exists:
			finalCount--
		case !s.deleted && !exists:
			finalCount++
		}
	}
	if finalCount > l.maxAccts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, s := range cand {
		if s.deleted {
			delete(l.accounts, name)
		} else {
			l.accounts[name] = s.acct
		}
	}
	l.revision = revision
	if len(b.Ops) > 0 {
		l.generation++
	}
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
	if n < len(all) {
		all = all[:n]
	}
	return all, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accts,
	}
}
