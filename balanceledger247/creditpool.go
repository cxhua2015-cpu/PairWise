package balanceledger247

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

// Ledger is a concurrency-safe in-memory balance ledger.
// A single RWMutex guards all state; writers are fully serialized,
// readers (Top/Snapshot/Stats/Clone/ValidateBatch) may run concurrently.
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

// Apply executes the batch atomically in input order. On any failure the
// ledger is left untouched. Add/Set assign consecutive revisions; the final
// account capacity is checked only at the end of the batch.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage mutations on a copy of the touched
	// accounts so a failure anywhere rolls the whole batch back.
	type staged struct {
		acc     Account
		existed bool
	}
	pending := make(map[string]staged)
	order := make([]string, 0, len(b.Ops))
	nextRev := l.nextRevision

	lookup := func(name string) (Account, bool) {
		if s, ok := pending[name]; ok {
			return s.acc, s.existed
		}
		a, ok := l.accounts[name]
		return a, ok
	}
	stage := func(name string, a Account, existed bool) {
		if _, ok := pending[name]; !ok {
			order = append(order, name)
		}
		pending[name] = staged{acc: a, existed: existed}
	}

	for _, op := range b.Ops {
		cur, existed := lookup(op.Name)
		switch op.Kind {
		case Add:
			// Detect int64 overflow before performing the arithmetic.
			nv, ov := add64(cur.Value, op.Delta)
			if !existed {
				nv, ov = add64(0, op.Delta)
			}
			if ov || exceedsLimit(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			stage(op.Name, Account{Name: op.Name, Value: nv, Revision: nextRev}, true)
			nextRev++
		case Set:
			if exceedsLimit(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			stage(op.Name, Account{Name: op.Name, Value: op.Value, Revision: nextRev}, true)
			nextRev++
		case Delete:
			if !existed {
				return Result{}, ErrNotFound
			}
			stage(op.Name, Account{}, false)
		}
	}

	// Final capacity check only, at the end of the batch.
	final := len(l.accounts)
	for _, s := range pending {
		if s.existed {
			final++
		} else {
			final--
		}
	}
	if final > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		s := pending[name]
		if s.existed {
			l.accounts[name] = s.acc
			changed = append(changed, s.acc)
		} else {
			delete(l.accounts, name)
		}
	}
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = nextRev
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
}

// Top returns up to n accounts ordered by value descending, name ascending.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := l.sortedLocked()
	if n > len(all) {
		n = len(all)
	}
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
}

// Snapshot returns a copy of the state with accounts sorted by name.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := l.sortedLocked()
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}

// sortedLocked returns accounts ordered by value desc, name asc. Caller holds the lock.
func (l *Ledger) sortedLocked() []Account {
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
	return accs
}

func add64(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, true
	}
	return s, false
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// exceedsLimit reports whether |v| > limit, safe for math.MinInt64 whose
// negation overflows int64.
func exceedsLimit(v, limit int64) bool {
	if v == -1<<63 {
		return true
	}
	return abs64(v) > limit
}
