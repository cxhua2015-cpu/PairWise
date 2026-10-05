package resourceledger152

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
	gen      uint64
	nextRev  uint64
	accounts map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, nextRev: 1, accounts: make(map[string]Account)}, nil
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
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Add && !absOK(op.Delta, l.opts.MaxAbsValue) {
			return Result{}, ErrValue
		}
		if op.Kind == Set && !absOK(op.Value, l.opts.MaxAbsValue) {
			return Result{}, ErrValue
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
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
		acc, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			v := acc.Value
			d := op.Delta
			if (d > 0 && v > math.MaxInt64-d) || (d < 0 && v < math.MinInt64-d) {
				return Result{}, ErrValue
			}
			if !absOK(v+d, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: v + d, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
		case Set:
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(touched))
	for _, name := range touched {
		if acc, ok := cand[name]; ok {
			changed = append(changed, acc)
		}
	}

	l.accounts = cand
	l.nextRev = nextRev
	l.gen++
	return Result{Generation: l.gen, Revision: nextRev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].Value != accs[j].Value {
			return accs[i].Value > accs[j].Value
		}
		return accs[i].Name < accs[j].Name
	})
	if n > len(accs) {
		n = len(accs)
	}
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: accs}
}
