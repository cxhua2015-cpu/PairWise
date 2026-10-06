package balanceledger272

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

// applyOp mutates the candidate account set; rev is the next revision and is
// consumed (returned incremented) by Add and Set.
func applyOp(cand map[string]Account, touched *[]string, o Op, rev uint64, maxAbs int64) (uint64, error) {
	switch o.Kind {
	case Add:
		a, ok := cand[o.Name]
		if !ok {
			a = Account{Name: o.Name}
			*touched = append(*touched, o.Name)
		}
		if (o.Delta > 0 && a.Value > math.MaxInt64-o.Delta) ||
			(o.Delta < 0 && a.Value < math.MinInt64-o.Delta) {
			return rev, ErrValue
		}
		v := a.Value + o.Delta
		if v > maxAbs || v < -maxAbs {
			return rev, ErrValue
		}
		a.Value = v
		a.Revision = rev
		cand[o.Name] = a
		return rev + 1, nil
	case Set:
		if _, ok := cand[o.Name]; !ok {
			*touched = append(*touched, o.Name)
		}
		cand[o.Name] = Account{Name: o.Name, Value: o.Value, Revision: rev}
		return rev + 1, nil
	case Delete:
		if _, ok := cand[o.Name]; !ok {
			return rev, ErrNotFound
		}
		delete(cand, o.Name)
		return rev, nil
	}
	return rev, ErrInvalidInput
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	touched := make([]string, 0, len(b.Ops))
	rev := l.nextRevision
	var err error
	for _, o := range b.Ops {
		rev, err = applyOp(cand, &touched, o, rev, l.opts.MaxAbsValue)
		if err != nil {
			return Result{}, err
		}
	}
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(touched))
	for _, name := range touched {
		if a, ok := cand[name]; ok {
			changed = append(changed, a)
		}
	}
	l.accounts = cand
	l.nextRevision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
