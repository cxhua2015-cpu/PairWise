package resourceledger112

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
	mu       sync.RWMutex
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
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validateOp(op Op, maxBytes int) error {
	switch op.Kind {
	case Add, Set, Delete:
	default:
		return ErrInvalidInput
	}
	if !validName(op.Name, maxBytes) {
		return ErrInvalidInput
	}
	return nil
}

func checkAbs(v, maxAbs int64) error {
	if v > maxAbs || v < -maxAbs {
		return ErrValue
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		if err := validateOp(op, l.opts.MaxNameBytes); err != nil {
			return Result{}, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: mutate a clone, commit on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	changed := make(map[string]Account)

	for _, op := range b.Ops {
		acc, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta ||
				op.Delta < 0 && acc.Value < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			acc.Value += op.Delta
			if err := checkAbs(acc.Value, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			acc.Name = op.Name
			acc.Revision = nextRev
			nextRev++
			cand[op.Name] = acc
			changed[op.Name] = acc
		case Set:
			if err := checkAbs(op.Value, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
			changed[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			delete(changed, op.Name)
		}
	}

	// Final account capacity is checked only at batch end.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	res := Result{Generation: l.gen, Revision: nextRev - 1}
	if len(b.Ops) > 0 {
		l.accounts = cand
		l.nextRev = nextRev
		l.gen++
		res.Generation = l.gen
		res.Changed = make([]Account, 0, len(changed))
		for _, acc := range changed {
			res.Changed = append(res.Changed, acc)
		}
		sort.Slice(res.Changed, func(i, j int) bool { return res.Changed[i].Name < res.Changed[j].Name })
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
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
	s := Snapshot{
		Generation:   l.gen,
		NextRevision: l.nextRev,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, acc := range l.accounts {
		s.Accounts = append(s.Accounts, acc)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
