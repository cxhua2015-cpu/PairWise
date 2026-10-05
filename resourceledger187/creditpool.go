package resourceledger187

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

func absOK(v, max int64) bool {
	if v < 0 {
		// max >= 1, so -max cannot overflow; v == math.MinInt64 fails here.
		return v >= -max
	}
	return v <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 1: full structural validation before reading state.
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

	// Phase 2: execute against a candidate copy; commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	revision := l.revision
	var changed []Account
	changedIdx := make(map[string]int)
	note := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a, ok := candidate[op.Name]
			if !ok {
				a = Account{Name: op.Name}
			}
			// Overflow-safe addition: check before computing.
			if (op.Delta > 0 && a.Value > 0 && a.Value > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && a.Value < 0 && a.Value < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			v := a.Value + op.Delta
			if !absOK(v, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			revision++
			a.Value, a.Revision = v, revision
			candidate[op.Name] = a
			note(a)
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			revision++
			a := Account{Name: op.Name, Value: op.Value, Revision: revision}
			candidate[op.Name] = a
			note(a)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	// Changed reflects accounts that still exist at batch end.
	kept := changed[:0]
	for _, a := range changed {
		if _, ok := candidate[a.Name]; ok {
			kept = append(kept, a)
		}
	}
	changed = kept

	// Final capacity check only at batch end.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = candidate
	l.revision = revision
	if len(b.Ops) > 0 {
		l.generation++
	}
	return Result{Generation: l.generation, Revision: revision, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
