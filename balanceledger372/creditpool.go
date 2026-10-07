package balanceledger372

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
	mu         sync.RWMutex
	opt        Options
	accounts   map[string]Account
	generation uint64
	nextRev    uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opt: o, accounts: make(map[string]Account), nextRev: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

func absOK(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Pass 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opt.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: mutate a clone, commit only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	touched := make([]string, 0, len(b.Ops))
	seen := make(map[string]bool, len(b.Ops))

	for _, op := range b.Ops {
		if !seen[op.Name] {
			seen[op.Name] = true
			touched = append(touched, op.Name)
		}
		switch op.Kind {
		case Add:
			acc := cand[op.Name]
			acc.Name = op.Name
			d := op.Delta
			if (d > 0 && acc.Value > math.MaxInt64-d) || (d < 0 && acc.Value < math.MinInt64-d) {
				return Result{}, ErrValue
			}
			nv := acc.Value + d
			if !absOK(nv, l.opt.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Value = nv
			acc.Revision = nextRev
			nextRev++
			cand[op.Name] = acc
		case Set:
			if !absOK(op.Value, l.opt.MaxAbsValue) {
				return Result{}, ErrValue
			}
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	// Final account capacity checked only at batch end.
	if len(cand) > l.opt.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.generation++
	l.nextRev = nextRev

	res := Result{Generation: l.generation, Revision: nextRev - 1}
	for _, name := range touched {
		if acc, ok := cand[name]; ok {
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
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	l.mu.RUnlock()
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].Value != accs[j].Value {
			return accs[i].Value > accs[j].Value
		}
		return accs[i].Name < accs[j].Name
	})
	if n < len(accs) {
		accs = accs[:n]
	}
	return accs, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRev}
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s.Accounts = append(s.Accounts, l.accounts[name])
	}
	return s
}
