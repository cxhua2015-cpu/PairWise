package balanceledger212

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

func absLimitOK(v, limit int64) bool {
	// limit > 0; v may be math.MinInt64, whose negation overflows.
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

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Phase 2: candidate transaction on a private copy; commit only on success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	touched := make(map[string]bool)
	var changed []Account

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			var nv int64
			if exists {
				// Detect int64 overflow before performing arithmetic.
				if (op.Delta > 0 && acc.Value > 0 && acc.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && acc.Value < 0 && acc.Value < -(1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				nv = acc.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if !absLimitOK(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
		case Set:
			if !absLimitOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		if !touched[op.Name] {
			touched[op.Name] = true
			changed = append(changed, acc)
		} else {
			for i := range changed {
				if changed[i].Name == op.Name {
					changed[i] = acc
					break
				}
			}
		}
	}

	// Final account capacity is checked only at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev

	// Changed reports surviving accounts only, in first-touch order.
	out := changed[:0]
	for _, acc := range changed {
		if cur, ok := l.accounts[acc.Name]; ok {
			out = append(out, cur)
		}
	}
	return Result{Generation: l.generation, Revision: l.nextRevision - 1, Changed: out}, nil
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
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
