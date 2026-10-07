package balanceledger342

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
	mu         sync.RWMutex
	maxAcct    int
	maxName    int
	maxAbs     int64
	generation uint64
	nextRev    uint64
	accounts   map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAcct:  o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		nextRev:  1,
		accounts: make(map[string]Account),
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

// validate checks the whole batch structurally before any state is read.
func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return ErrInvalidInput
		}
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validate(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: work on a copy, commit only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	lastRev := l.nextRev - 1
	var changed []Account
	seen := make(map[string]int)

	record := func(a Account) {
		if i, ok := seen[a.Name]; ok {
			changed[i] = a
		} else {
			seen[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a, ok := cand[op.Name]
			if !ok {
				a = Account{Name: op.Name}
			}
			// Detect int64 overflow before arithmetic.
			if (op.Delta > 0 && a.Value > 0 && a.Value > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && a.Value < 0 && a.Value < -(1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			v := a.Value + op.Delta
			if v > l.maxAbs || v < -l.maxAbs {
				return Result{}, ErrValue
			}
			a.Value = v
			a.Revision = nextRev
			nextRev++
			lastRev = a.Revision
			cand[a.Name] = a
			record(a)
		case Set:
			if op.Value > l.maxAbs || op.Value < -l.maxAbs {
				return Result{}, ErrValue
			}
			a := cand[op.Name]
			a.Name = op.Name
			a.Value = op.Value
			a.Revision = nextRev
			nextRev++
			lastRev = a.Revision
			cand[a.Name] = a
			record(a)
		case Delete:
			a, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok := seen[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(seen, op.Name)
				for j := i; j < len(changed); j++ {
					seen[changed[j].Name] = j
				}
			}
			_ = a
		}
	}

	// Final account capacity is checked only at the end of the batch.
	if len(cand) > l.maxAcct {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRev = nextRev
	l.generation++
	return Result{Generation: l.generation, Revision: lastRev, Changed: changed}, nil
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
	if n < len(all) {
		all = all[:n]
	}
	return all, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRev,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool {
		return s.Accounts[i].Name < s.Accounts[j].Name
	})
	return s
}
