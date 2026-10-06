package balanceledger267

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
	mu           sync.Mutex
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

// applyLocked executes an already validated batch against a candidate copy of
// the state. On any error the candidate is discarded and nothing changes.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	revision := l.nextRevision
	changed := make(map[string]int)
	var changedList []Account

	record := func(a Account) {
		if i, ok := changed[a.Name]; ok {
			changedList[i] = a
			return
		}
		changed[a.Name] = len(changedList)
		changedList = append(changedList, a)
	}

	for _, op := range b.Ops {
		a, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if !exists {
				a = Account{Name: op.Name}
			}
			if (op.Delta > 0 && a.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && a.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			a.Value += op.Delta
			if a.Value > l.opts.MaxAbsValue || a.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a.Revision = revision
			revision++
			candidate[op.Name] = a
			record(a)
		case Set:
			a = Account{Name: op.Name, Value: op.Value, Revision: revision}
			revision++
			candidate[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if i, ok := changed[op.Name]; ok {
				changedList = append(changedList[:i], changedList[i+1:]...)
				delete(changed, op.Name)
				for j := i; j < len(changedList); j++ {
					changed[changedList[j].Name] = j
				}
			}
		}
	}
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = revision
	return Result{Generation: l.generation, Revision: revision - 1, Changed: changedList}, nil
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
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision,
		Accounts: make([]Account, 0, len(l.accounts))}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
