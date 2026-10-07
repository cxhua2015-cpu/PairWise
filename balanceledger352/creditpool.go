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
	mu         sync.RWMutex
	maxAccts   int
	maxName    int
	maxAbs     int64
	generation uint64
	nextRev    uint64 // next revision to assign, starts at 1
	accounts   map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccts: o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		nextRev:  1,
		accounts: make(map[string]Account),
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

func absOK(v, maxAbs int64) bool {
	if v < 0 {
		if v == -1<<63 {
			return false
		}
		v = -v
	}
	return v <= maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: mutate a clone, commit on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	changedIdx := make(map[string]int)
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if !absOK(op.Delta, l.maxAbs) {
				return Result{}, ErrValue
			}
			acc, ok := cand[op.Name]
			base := int64(0)
			if ok {
				base = acc.Value
			}
			// Overflow check before arithmetic.
			if (op.Delta > 0 && base > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && base < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if !absOK(nv, l.maxAbs) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
			if i, seen := changedIdx[op.Name]; seen {
				changed[i] = acc
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, acc)
			}
		case Set:
			if !absOK(op.Value, l.maxAbs) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
			if i, seen := changedIdx[op.Name]; seen {
				changed[i] = acc
			} else {
				changedIdx[op.Name] = len(changed)
				changed = append(changed, acc)
			}
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	// Final capacity check only at batch end.
	if len(cand) > l.maxAccts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRev = nextRev
	l.generation++
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
	out := make([]Account, n)
	copy(out, accs[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRev, Accounts: accs}
}
