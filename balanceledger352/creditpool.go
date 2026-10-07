package balanceledger352

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
	mu         sync.Mutex
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

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

func absOK(v, max int64) bool {
	if v < 0 {
		// max >= 1 so -max cannot overflow int64
		return v >= -max
	}
	return v <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Add && !absOK(op.Delta, l.maxAbs) {
			return Result{}, ErrValue
		}
		if op.Kind == Set && !absOK(op.Value, l.maxAbs) {
			return Result{}, ErrValue
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 2: candidate transaction on a clone; rollback is implicit.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRev
	touched := make(map[string]bool)
	var order []string

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			a, ok := cand[op.Name]
			if !ok {
				a = Account{Name: op.Name}
			}
			nv := a.Value + op.Delta
			// overflow check: signs of operands and result
			if (op.Delta > 0 && nv < a.Value) || (op.Delta < 0 && nv > a.Value) {
				return Result{}, ErrValue
			}
			if !absOK(nv, l.maxAbs) {
				return Result{}, ErrValue
			}
			a.Value = nv
			a.Revision = rev
			rev++
			cand[op.Name] = a
		case Set:
			a, ok := cand[op.Name]
			if !ok {
				a = Account{Name: op.Name}
			}
			a.Value = op.Value
			a.Revision = rev
			rev++
			cand[op.Name] = a
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
		if !touched[op.Name] {
			touched[op.Name] = true
			order = append(order, op.Name)
		}
	}

	// Final capacity check only at batch end.
	if len(cand) > l.maxAcct {
		return Result{}, ErrCapacity
	}

	// Commit.
	res := Result{Generation: l.generation, Revision: l.nextRev - 1}
	if len(b.Ops) > 0 {
		l.accounts = cand
		l.nextRev = rev
		l.generation++
		res.Generation = l.generation
		res.Revision = rev - 1
		for _, n := range order {
			if a, ok := cand[n]; ok {
				res.Changed = append(res.Changed, a)
			}
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
	}
	l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRev,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
