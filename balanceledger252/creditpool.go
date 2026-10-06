package balanceledger252

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

// Ledger is a concurrency-safe in-memory balance ledger.
// All public methods may be called concurrently.
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
	return &Ledger{
		opts:         o,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

// Apply executes the batch atomically in input order. On any failure the
// ledger is left untouched. A non-empty successful batch bumps the
// generation exactly once; an empty batch changes nothing.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: mutate a private copy, commit on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	var changed []Account
	changedIdx := make(map[string]int)

	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if exists {
				if op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta {
					return Result{}, ErrValue
				}
				if op.Delta < 0 && cur.Value < math.MinInt64-op.Delta {
					return Result{}, ErrValue
				}
			}
			nv := cur.Value + op.Delta
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Set:
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.nextRevision = nextRev
	l.generation++
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, then name ascending.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
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
	if n < len(all) {
		all = all[:n]
	}
	return all, nil
}

// Snapshot returns a name-sorted, fully detached copy of the state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	if len(l.accounts) > 0 {
		s.Accounts = make([]Account, 0, len(l.accounts))
		for _, a := range l.accounts {
			s.Accounts = append(s.Accounts, a)
		}
		sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	}
	return s
}
