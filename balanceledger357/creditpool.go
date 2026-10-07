package balanceledger357

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
type Ledger struct {
	mu         sync.RWMutex
	accounts   map[string]Account
	generation uint64
	revision   uint64
	maxAccount int
	maxName    int
	maxAbs     int64
}

// New creates a Ledger. All option limits must be positive.
func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		accounts:   make(map[string]Account),
		maxAccount: o.MaxAccounts,
		maxName:    o.MaxNameBytes,
		maxAbs:     o.MaxAbsValue,
	}, nil
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

func absOK(v, maxAbs int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= maxAbs
}

// Apply executes the batch atomically in input order. Add/Set allocate
// consecutive revisions. On any error the ledger is left unchanged.
func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation, Revision: l.revision}
		l.mu.RUnlock()
		return r, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: apply onto a clone, commit on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
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
			if !absOK(op.Delta, l.maxAbs) {
				return Result{}, ErrValue
			}
			old, ok := cand[op.Name]
			if ok {
				if (op.Delta > 0 && old.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && old.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
			}
			nv := old.Value + op.Delta
			if !absOK(nv, l.maxAbs) {
				return Result{}, ErrValue
			}
			rev++
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			cand[op.Name] = a
			note(a)
		case Set:
			if !absOK(op.Value, l.maxAbs) {
				return Result{}, ErrValue
			}
			rev++
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			cand[op.Name] = a
			note(a)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	if len(cand) > l.maxAccount {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.revision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, name ascending.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
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

// Snapshot returns a copy of the ledger state, accounts sorted by name.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
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
