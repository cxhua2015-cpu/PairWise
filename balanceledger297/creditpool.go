package balanceledger297

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
	mu      sync.RWMutex
	opts    Options
	accts   map[string]Account
	gen     uint64
	nextRev uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accts: make(map[string]Account), nextRev: 1}, nil
}

// checkAbs reports whether |v| exceeds the configured absolute limit,
// detecting int64 overflow before any arithmetic on v.
func (l *Ledger) checkAbs(v int64) bool {
	m := l.opts.MaxAbsValue
	if v >= 0 {
		return v <= m
	}
	// v < 0; -v may overflow only for math.MinInt64.
	return v >= -m
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: work on a private copy so failure rolls back.
	cand := make(map[string]Account, len(l.accts)+len(b.Ops))
	for k, v := range l.accts {
		cand[k] = v
	}
	nextRev := l.nextRev
	touched := make(map[string]int) // name -> index in changed
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a, ok := cand[op.Name]
			if ok {
				// Detect int64 overflow before performing the addition.
				if (op.Delta > 0 && a.Value > 0 && a.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && a.Value < 0 && a.Value < (-1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				a.Value += op.Delta
			} else {
				a = Account{Name: op.Name, Value: op.Delta}
			}
			if !l.checkAbs(a.Value) {
				return Result{}, ErrValue
			}
			a.Revision = nextRev
			nextRev++
			cand[op.Name] = a
			if i, seen := touched[op.Name]; seen {
				changed[i] = a
			} else {
				touched[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Set:
			if !l.checkAbs(op.Value) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			if i, seen := touched[op.Name]; seen {
				changed[i] = a
			} else {
				touched[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(touched, op.Name)
			for i, c := range changed {
				if c.Name == op.Name {
					changed = append(changed[:i], changed[i+1:]...)
					break
				}
			}
			// Rebuild index to stay correct after removal.
			for i, c := range changed {
				touched[c.Name] = i
			}
		}
	}

	// Final account capacity is only checked at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accts = cand
	l.nextRev = nextRev
	if len(b.Ops) > 0 {
		l.gen++
	}
	res := Result{Generation: l.gen, Revision: nextRev - 1, Changed: changed}
	if len(b.Ops) == 0 {
		res.Changed = nil
	}
	return res, nil
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
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: make([]Account, 0, len(l.accts))}
	for _, a := range l.accts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
