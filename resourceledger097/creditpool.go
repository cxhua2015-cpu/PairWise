package resourceledger097

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
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
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

// absOK reports whether |v| <= limit without overflowing on math.MinInt64.
func absOK(v, limit int64) bool {
	if v >= 0 {
		return v <= limit
	}
	// v < 0; avoid -v overflow by comparing against negated limit (limit > 0).
	return v >= -limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
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
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Candidate transaction: stage changes on a clone; commit only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
	touched := make(map[string]bool)
	var order []string

	for _, op := range b.Ops {
		acc, exists := cand[op.Name]
		switch op.Kind {
		case Add:
			if exists {
				if (op.Delta > 0 && acc.Value > 0 && acc.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && acc.Value < 0 && acc.Value < -(1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				nv := acc.Value + op.Delta
				if !absOK(nv, l.opts.MaxAbsValue) {
					return Result{}, ErrValue
				}
				acc.Value = nv
			} else {
				if !absOK(op.Delta, l.opts.MaxAbsValue) {
					return Result{}, ErrValue
				}
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			rev++
			acc.Revision = rev
			cand[op.Name] = acc
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			rev++
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: rev}
		case Delete:
			if !exists {
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
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = cand
	l.revision = rev
	l.generation++

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if acc, ok := cand[name]; ok {
			changed = append(changed, acc)
		}
	}
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
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
	return Snapshot{Generation: l.generation, NextRevision: l.revision + 1, Accounts: accs}
}
