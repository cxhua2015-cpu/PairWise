package resourceledger082

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
	mu      sync.RWMutex
	opts    Options
	accts   map[string]Account
	gen     uint64
	nextRev uint64
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accts: make(map[string]Account), nextRev: 1}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	res := Result{Generation: l.gen}
	if l.nextRev > 0 {
		res.Revision = l.nextRev - 1
	}
	if len(b.Ops) == 0 {
		return res, nil
	}

	// Candidate transaction: clone the accounts map so any failure
	// leaves the committed state untouched.
	cand := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		cand[k] = v
	}
	nextRev := l.nextRev
	changedIdx := map[string]int{}
	var changed []Account

	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			var nv int64
			if ok {
				d := op.Delta
				if (d > 0 && cur.Value > math.MaxInt64-d) || (d < 0 && cur.Value < math.MinInt64-d) {
					return Result{}, ErrValue
				}
				nv = cur.Value + d
			} else {
				nv = op.Delta
			}
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			if i, seen := changedIdx[op.Name]; seen {
				changed[i] = a
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			if i, seen := changedIdx[op.Name]; seen {
				changed[i] = a
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	// Final account capacity is only checked at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accts = cand
	l.nextRev = nextRev
	l.gen++
	res.Generation = l.gen
	res.Revision = nextRev - 1
	res.Changed = changed
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
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev}
	s.Accounts = make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
