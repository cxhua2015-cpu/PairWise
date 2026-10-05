package resourceledger117

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
	mu      sync.RWMutex
	opts    Options
	accts   map[string]Account
	gen     uint64
	nextRev uint64
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accts: make(map[string]Account), nextRev: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
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

	// Candidate transaction: work on a clone, commit on success.
	cand := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		cand[k] = v
	}
	nextRev := l.nextRev
	var changed []Account
	changedIdx := make(map[string]int)
	note := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	unnote := func(name string) {
		if i, ok := changedIdx[name]; ok {
			changed = append(changed[:i], changed[i+1:]...)
			delete(changedIdx, name)
			for j := i; j < len(changed); j++ {
				changedIdx[changed[j].Name] = j
			}
		}
	}

	for _, op := range b.Ops {
		a, exists := cand[op.Name]
		switch op.Kind {
		case Add:
			v := a.Value
			if exists {
				if (op.Delta > 0 && v > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && v < (-1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				v += op.Delta
			} else {
				v = op.Delta
			}
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: v, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			note(a)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			note(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			unnote(op.Name)
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.gen++
	}
	l.accts = cand
	l.nextRev = nextRev
	return Result{Generation: l.gen, Revision: nextRev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
		all = append(all, a)
	}
	l.mu.RUnlock()
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
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev,
		Accounts: make([]Account, 0, len(l.accts))}
	for _, a := range l.accts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
