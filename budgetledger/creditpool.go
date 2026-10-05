package budgetledger

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
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		accounts:     make(map[string]Account),
		nextRevision: 1,
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
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
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: mutate a copy; commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account

	record := func(a Account) {
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
			cur, ok := candidate[op.Name]
			var nv int64
			if ok {
				nv = cur.Value
			}
			if (op.Delta > 0 && nv > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && nv < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv += op.Delta
			if nv > l.maxAbsValue || nv < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	// Account capacity is checked only at the end of the batch.
	if len(candidate) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
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
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	accs := make([]Account, 0, len(names))
	for _, name := range names {
		accs = append(accs, l.accounts[name])
	}
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
