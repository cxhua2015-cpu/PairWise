package costledger

import (
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
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
	mu       sync.Mutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRev: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func absOK(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	av := v
	if av < 0 {
		av = -av
	}
	return av <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Full structural validation before touching state.
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

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: mutate a clone, commit by swap.
	cand := maps.Clone(l.accounts)
	nextRev := l.nextRev
	changed := make(map[string]struct{})

	for _, op := range b.Ops {
		acc, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			if ok {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				acc.Value += op.Delta
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if !absOK(acc.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Revision = nextRev
			nextRev++
			cand[op.Name] = acc
			changed[op.Name] = struct{}{}
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
			changed[op.Name] = struct{}{}
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			changed[op.Name] = struct{}{}
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = cand
	l.nextRev = nextRev
	l.gen++

	res := Result{Generation: l.gen, Revision: nextRev - 1}
	names := make([]string, 0, len(changed))
	for n := range changed {
		if _, ok := cand[n]; ok {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	for _, n := range names {
		res.Changed = append(res.Changed, cand[n])
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	slices.SortFunc(accs, func(x, y Account) int {
		if x.Value != y.Value {
			if x.Value > y.Value {
				return -1
			}
			return 1
		}
		return strings.Compare(x.Name, y.Name)
	})
	if n > len(accs) {
		n = len(accs)
	}
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev}
	names := make([]string, 0, len(l.accounts))
	for n := range l.accounts {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		s.Accounts = append(s.Accounts, l.accounts[n])
	}
	return s
}
