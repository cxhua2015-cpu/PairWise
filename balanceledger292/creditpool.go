package balanceledger292

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
//
// A single RWMutex guards one name-indexed account map. Batches run as
// candidate transactions: mutations are staged on private copies of the
// touched accounts and committed only after every structural, value and
// capacity check has passed, so a failed batch leaves no observable trace.
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

func absWithin(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}

// Apply executes a batch atomically, in input order. Add and Set assign
// consecutive revisions; the account-capacity limit is checked only against
// the final state of the batch. Any failure rolls the whole batch back.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction state.
	type entry struct {
		acc     Account
		exists  bool
		deleted bool
	}
	staged := make(map[string]*entry)
	stage := func(name string) *entry {
		if e, ok := staged[name]; ok {
			return e
		}
		a, ok := l.accounts[name]
		e := &entry{acc: a, exists: ok}
		staged[name] = e
		return e
	}
	changed := make([]Account, 0, len(b.Ops))
	changedIdx := make(map[string]int)
	note := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}
	unote := func(name string) {
		i, ok := changedIdx[name]
		if !ok {
			return
		}
		changed = append(changed[:i], changed[i+1:]...)
		delete(changedIdx, name)
		for n, idx := range changedIdx {
			if idx > i {
				changedIdx[n] = idx - 1
			}
		}
	}

	rev := l.nextRevision
	size := len(l.accounts)
	for _, op := range b.Ops {
		e := stage(op.Name)
		switch op.Kind {
		case Add:
			if op.Delta > 0 && e.acc.Value > math.MaxInt64-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && e.acc.Value < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			nv := e.acc.Value + op.Delta
			if !absWithin(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			if !e.exists || e.deleted {
				size++
			}
			e.acc = Account{Name: op.Name, Value: nv, Revision: rev}
			e.exists, e.deleted = true, false
			rev++
			note(e.acc)
		case Set:
			if !absWithin(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			if !e.exists || e.deleted {
				size++
			}
			e.acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			e.exists, e.deleted = true, false
			rev++
			note(e.acc)
		case Delete:
			if !e.exists || e.deleted {
				return Result{}, ErrNotFound
			}
			size--
			e.deleted = true
			e.acc = Account{Name: op.Name}
			unote(op.Name)
		}
	}
	if size > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, e := range staged {
		if e.deleted {
			delete(l.accounts, name)
		} else if e.exists {
			l.accounts[name] = e.acc
		}
	}
	l.generation++
	l.nextRevision = rev
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, then name
// ascending. The returned slice is detached from internal state.
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
	return all[:n], nil
}

// Snapshot returns a consistent view sorted by account name.
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
