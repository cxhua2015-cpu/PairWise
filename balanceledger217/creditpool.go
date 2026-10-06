package balanceledger217

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
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRevision: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation, Revision: l.nextRevision - 1}
		l.mu.RUnlock()
		return r, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: clone the account map so rollback is free.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			v := cur.Value
			if (op.Delta > 0 && v > 0 && v > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && v < 0 && v < -(1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			v += op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			cand[op.Name] = cur
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = cur
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
		if op.Kind != Delete {
			if i, seen := changedIdx[op.Name]; seen {
				changed[i] = cur
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, cur)
			}
		} else {
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = cand
	l.generation++
	l.nextRevision = rev
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
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
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
