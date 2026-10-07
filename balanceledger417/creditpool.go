package balanceledger417

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
// Internally it keeps a single authoritative index (map name -> Account)
// guarded by a RWMutex. Apply runs as a candidate transaction: the batch
// is first validated structurally without touching state, then executed
// against a private candidate index; the candidate is committed only if
// the whole batch, including the final capacity check, succeeds.
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: structural validation only; must not read state.
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Phase 2: candidate transaction on a private copy of the index.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	changed := make(map[string]Account)
	order := make([]string, 0, len(b.Ops))

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			// Detect int64 overflow before doing any arithmetic.
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := acc.Value + op.Delta
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
		case Set:
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if _, ok := changed[op.Name]; !ok {
				order = append(order, op.Name)
			}
			changed[op.Name] = Account{Name: op.Name}
			continue
		}
		candidate[op.Name] = acc
		if _, ok := changed[op.Name]; !ok {
			order = append(order, op.Name)
		}
		changed[op.Name] = acc
	}

	// Final capacity check happens only at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit: swap in the candidate index and advance the clocks once.
	l.accounts = candidate
	l.nextRevision = nextRev
	l.generation++

	res := Result{Generation: l.generation, Revision: nextRev - 1}
	res.Changed = make([]Account, 0, len(order))
	for _, name := range order {
		if acc, ok := candidate[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := sortedAccounts(l.accounts, func(a, b Account) bool {
		if a.Value != b.Value {
			return a.Value > b.Value // value descending
		}
		return a.Name < b.Name // name ascending
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
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     sortedAccounts(l.accounts, func(a, b Account) bool { return a.Name < b.Name }),
	}
}

// sortedAccounts returns a fresh slice of copies; callers own the result.
func sortedAccounts(m map[string]Account, less func(a, b Account) bool) []Account {
	out := make([]Account, 0, len(m))
	for _, a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
