package balanceledger332

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
	// Structural validation of the whole batch before touching state.
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

	maxAbs := l.opts.MaxAbsValue
	// Candidate transaction: clone the account map so failure is a no-op.
	cand := make(map[string]Account, len(l.accts))
	for k, v := range l.accts {
		cand[k] = v
	}
	nextRev := l.nextRev
	touched := make(map[string]bool)
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur := cand[op.Name]
			// Detect int64 overflow before doing arithmetic.
			if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := cur.Value + op.Delta
			if nv > maxAbs || nv < -maxAbs {
				return Result{}, ErrValue
			}
			cur.Name = op.Name
			cur.Value = nv
			cur.Revision = nextRev
			nextRev++
			cand[op.Name] = cur
			if !touched[op.Name] {
				touched[op.Name] = true
				changed = append(changed, Account{Name: op.Name})
			}
		case Set:
			if op.Value > maxAbs || op.Value < -maxAbs {
				return Result{}, ErrValue
			}
			cur := cand[op.Name]
			cur.Name = op.Name
			cur.Value = op.Value
			cur.Revision = nextRev
			nextRev++
			cand[op.Name] = cur
			if !touched[op.Name] {
				touched[op.Name] = true
				changed = append(changed, Account{Name: op.Name})
			}
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	// Final account capacity is checked only at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accts = cand
	l.nextRev = nextRev
	if len(b.Ops) > 0 {
		l.gen++
	}
	for i := range changed {
		changed[i] = cand[changed[i].Name]
	}
	// Accounts deleted within the batch are dropped from Changed.
	out := changed[:0]
	for _, a := range changed {
		if _, ok := cand[a.Name]; ok {
			out = append(out, a)
		}
	}
	return Result{Generation: l.gen, Revision: nextRev - 1, Changed: out}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
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
	res := make([]Account, n)
	copy(res, all[:n])
	return res, nil
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
