package balanceledger392

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
	mu           sync.Mutex
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
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
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
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

	// Candidate transaction: mutate a copy, commit only on success.
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	record := func(a Account) {
		if i, seen := changedIdx[a.Name]; seen {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	unrecord := func(name string) {
		i, seen := changedIdx[name]
		if !seen {
			return
		}
		delete(changedIdx, name)
		changed = append(changed[:i], changed[i+1:]...)
		for j := i; j < len(changed); j++ {
			changedIdx[changed[j].Name] = j
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur, ok := cand[op.Name]
			nv := op.Delta
			if ok {
				if (op.Delta > 0 && cur.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && cur.Value < -(1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			}
			if nv > l.maxAbsValue || nv < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			unrecord(op.Name)
		}
	}
	// Final account capacity is checked only at the end of the batch.
	if len(cand) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRevision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
