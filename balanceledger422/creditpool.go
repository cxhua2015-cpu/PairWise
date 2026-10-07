package balanceledger422

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
// The zero value is not usable; construct with New.
type Ledger struct {
	mu      sync.RWMutex
	opts    Options // immutable after New
	gen     uint64
	nextRev uint64
	accts   map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, nextRev: 1, accts: make(map[string]Account)}, nil
}

// applyLocked executes an already validated batch against l.
// l.mu must be held for writing.
func (l *Ledger) applyLocked(b Batch) (Result, error) {
	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}
	work := make(map[string]Account, len(l.accts)+len(b.Ops))
	for k, v := range l.accts {
		work[k] = v
	}
	rev := l.nextRev
	var changed []Account
	dropped := make([]bool, 0, len(b.Ops))
	idx := make(map[string]int, len(b.Ops))
	record := func(a Account) {
		if i, ok := idx[a.Name]; ok {
			changed[i] = a
			dropped[i] = false
			return
		}
		idx[a.Name] = len(changed)
		changed = append(changed, a)
		dropped = append(dropped, false)
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur, ok := work[op.Name]
			nv := op.Delta
			if ok {
				if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			}
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: nv, Revision: rev}
			work[op.Name] = cur
			record(cur)
			rev++
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			work[op.Name] = a
			record(a)
			rev++
		case Delete:
			if _, ok := work[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(work, op.Name)
			if i, ok := idx[op.Name]; ok {
				dropped[i] = true
				delete(idx, op.Name)
			}
		}
	}
	if len(work) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	final := make([]Account, 0, len(changed))
	for i, a := range changed {
		if !dropped[i] {
			final = append(final, a)
		}
	}
	l.accts = work
	l.nextRev = rev
	l.gen++
	return Result{Generation: l.gen, Revision: rev - 1, Changed: final}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.applyLocked(b)
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
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
	out := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: out}
}
