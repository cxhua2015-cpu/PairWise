package resourceledger092

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
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
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	lastRevision := l.nextRevision - 1
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: lastRevision}, nil
	}

	// Candidate transaction: staged writes/deletes overlaid on committed state.
	staged := make(map[string]Account)
	deleted := make(map[string]bool)
	seen := make(map[string]bool)
	var order []string
	nextRev := l.nextRevision

	lookup := func(name string) (Account, bool) {
		if a, ok := staged[name]; ok {
			return a, true
		}
		if deleted[name] {
			return Account{}, false
		}
		a, ok := l.accounts[name]
		return a, ok
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur, exists := lookup(op.Name)
			var nv int64
			if exists {
				if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if nv > l.maxAbsValue || nv < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			if !seen[op.Name] {
				seen[op.Name] = true
				order = append(order, op.Name)
			}
			staged[op.Name] = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			if !seen[op.Name] {
				seen[op.Name] = true
				order = append(order, op.Name)
			}
			delete(deleted, op.Name)
			staged[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
		case Delete:
			if _, exists := lookup(op.Name); !exists {
				return Result{}, ErrNotFound
			}
			delete(staged, op.Name)
			deleted[op.Name] = true
		}
	}

	// Account capacity is checked only against the final state of the batch.
	count := len(l.accounts)
	for name := range deleted {
		if _, ok := l.accounts[name]; ok {
			count--
		}
	}
	for name := range staged {
		if _, ok := l.accounts[name]; !ok {
			count++
		}
	}
	if count > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name := range deleted {
		delete(l.accounts, name)
	}
	for name, a := range staged {
		l.accounts[name] = a
	}
	l.generation++
	l.nextRevision = nextRev

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if a, ok := staged[name]; ok {
			changed = append(changed, a)
		}
	}
	return Result{Generation: l.generation, Revision: l.nextRevision - 1, Changed: changed}, nil
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
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
