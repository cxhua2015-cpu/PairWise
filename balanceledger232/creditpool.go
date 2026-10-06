package balanceledger232

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

type accountState struct {
	value    int64
	revision uint64
}

type Ledger struct {
	mu       sync.RWMutex
	opts     Options
	accounts map[string]accountState
	// generation counts successful non-empty batches; nextRev is the next
	// revision to assign (revisions start at 1).
	generation, nextRev uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]accountState), nextRev: 1}, nil
}

// applyOp mutates the candidate state for a single op, assigning revisions
// from *nextRev. It assumes the batch has already passed structural
// validation.
func applyOp(cand map[string]accountState, op Op, maxAbs int64, nextRev *uint64) error {
	st, ok := cand[op.Name]
	switch op.Kind {
	case Add:
		v := st.value
		if (op.Delta > 0 && v > math.MaxInt64-op.Delta) || (op.Delta < 0 && v < math.MinInt64-op.Delta) {
			return ErrValue
		}
		v += op.Delta
		if v > maxAbs || v < -maxAbs {
			return ErrValue
		}
		st.value = v
		st.revision = *nextRev
		*nextRev++
		cand[op.Name] = st
	case Set:
		if op.Value > maxAbs || op.Value < -maxAbs {
			return ErrValue
		}
		st.value = op.Value
		st.revision = *nextRev
		*nextRev++
		cand[op.Name] = st
	case Delete:
		if !ok {
			return ErrNotFound
		}
		delete(cand, op.Name)
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]accountState, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	var changed []Account
	seen := make(map[string]int)
	for _, op := range b.Ops {
		if err := applyOp(cand, op, l.opts.MaxAbsValue, &nextRev); err != nil {
			return Result{}, err
		}
		if op.Kind == Delete {
			continue
		}
		st := cand[op.Name]
		if i, ok := seen[op.Name]; ok {
			changed[i] = Account{Name: op.Name, Value: st.value, Revision: st.revision}
		} else {
			seen[op.Name] = len(changed)
			changed = append(changed, Account{Name: op.Name, Value: st.value, Revision: st.revision})
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}
	l.accounts = cand
	l.generation++
	l.nextRev = nextRev
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
}

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

// sortedLocked returns accounts ordered by value descending, then name
// ascending. Callers must hold at least a read lock.
func (l *Ledger) sortedLocked() []Account {
	all := make([]Account, 0, len(l.accounts))
	for name, st := range l.accounts {
		all = append(all, Account{Name: name, Value: st.value, Revision: st.revision})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value > all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	return all
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := l.sortedLocked()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRev, Accounts: all}
}
