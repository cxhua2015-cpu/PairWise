package balanceledger262

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

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func absOK(v, limit int64) bool {
	if v < 0 {
		if v == -v { // MinInt64
			return false
		}
		return -v <= limit
	}
	return v <= limit
}

// validateOp checks structural semantics of a single op (no state access).
func validateOp(o Options, op Op) error {
	switch op.Kind {
	case Add:
		if !validName(op.Name, o.MaxNameBytes) || op.Delta == 0 {
			return ErrInvalidInput
		}
		if !absOK(op.Delta, o.MaxAbsValue) {
			return ErrValue
		}
	case Set:
		if !validName(op.Name, o.MaxNameBytes) {
			return ErrInvalidInput
		}
		if !absOK(op.Value, o.MaxAbsValue) {
			return ErrValue
		}
	case Delete:
		if !validName(op.Name, o.MaxNameBytes) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		r := Result{Generation: l.generation, Revision: l.nextRevision - 1}
		l.mu.RUnlock()
		return r, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Work on a candidate copy; commit only on full success.
	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	changedOrder := []string{}
	changedSet := map[string]bool{}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := candidate[op.Name]
			base := int64(0)
			if ok {
				base = acc.Value
			}
			// Detect int64 overflow before arithmetic.
			if (op.Delta > 0 && base > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && base < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if !absOK(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Name = op.Name
			acc.Value = nv
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			if !changedSet[op.Name] {
				changedSet[op.Name] = true
				changedOrder = append(changedOrder, op.Name)
			}
		case Set:
			acc := candidate[op.Name]
			acc.Name = op.Name
			acc.Value = op.Value
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			if !changedSet[op.Name] {
				changedSet[op.Name] = true
				changedOrder = append(changedOrder, op.Name)
			}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			changedSet[op.Name] = true
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(changedOrder))
	for _, name := range changedOrder {
		if acc, ok := candidate[name]; ok {
			changed = append(changed, acc)
		}
	}

	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
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
