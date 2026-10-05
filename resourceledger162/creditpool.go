package resourceledger162

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
	if len(name) == 0 || len(name) > maxBytes {
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

func absLimit(v, limit int64) bool {
	// limit > 0; v may be MinInt64 whose negation overflows.
	return v <= limit && v >= -limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
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

	// Phase 2: execute against a candidate copy; discard on any failure.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRev
	var changed []Account
	changedIdx := make(map[string]int)
	record := func(acc Account) {
		if i, ok := changedIdx[acc.Name]; ok {
			changed[i] = acc
			return
		}
		changedIdx[acc.Name] = len(changed)
		changed = append(changed, acc)
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc := cand[op.Name]
			// Detect int64 overflow before performing the addition.
			if (op.Delta > 0 && acc.Value > 0 && acc.Value > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && acc.Value < 0 && acc.Value < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := acc.Value + op.Delta
			if !absLimit(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Name = op.Name
			acc.Value = nv
			acc.Revision = rev
			rev++
			cand[op.Name] = acc
			record(acc)
		case Set:
			if !absLimit(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = acc
			record(acc)
		case Delete:
			acc, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			record(acc)
		}
	}
	// Final account capacity is checked only at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	if len(b.Ops) > 0 {
		l.gen++
	}
	l.nextRev = rev
	return Result{Generation: l.gen, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: accs}
}
