package balanceledger402

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
	generation   uint64
	nextRevision uint64
	accounts     map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, nextRevision: 1, accounts: make(map[string]Account)}, nil
}

func absExceeds(v, limit int64) bool {
	if v == math.MinInt64 {
		return true
	}
	if v < 0 {
		v = -v
	}
	return v > limit
}

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
	nextRev := l.nextRevision
	touched := make(map[string]int)
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if absExceeds(op.Delta, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc, ok := candidate[op.Name]
			if !ok {
				acc = Account{Name: op.Name}
			}
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			acc.Value += op.Delta
			if absExceeds(acc.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			if i, seen := touched[op.Name]; seen {
				changed[i] = acc
			} else {
				touched[op.Name] = len(changed)
				changed = append(changed, acc)
			}
		case Set:
			if absExceeds(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
			if i, seen := touched[op.Name]; seen {
				changed[i] = acc
			} else {
				touched[op.Name] = len(changed)
				changed = append(changed, acc)
			}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if i, seen := touched[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(touched, op.Name)
				for j := i; j < len(changed); j++ {
					touched[changed[j].Name] = j
				}
			}
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	if len(b.Ops) > 0 {
		l.generation++
	}
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
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
