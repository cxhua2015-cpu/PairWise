package resourceledger167

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
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func checkAbs(v, maxAbs int64) bool {
	return v <= maxAbs && v >= -maxAbs
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

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1}
	if len(b.Ops) == 0 {
		return res, nil
	}

	// Candidate transaction: mutate a clone, commit on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := cand[op.Name]
			// Overflow check before any arithmetic.
			if ok && (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta ||
				op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := acc.Value + op.Delta
			if !checkAbs(nv, l.opts.MaxAbsValue) {
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
			if !checkAbs(op.Value, l.opts.MaxAbsValue) {
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
			delete(changedIdx, op.Name)
		}
	}

	// Final account capacity is only checked at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.generation++
	l.nextRevision = nextRev
	res.Generation = l.generation
	res.Revision = nextRev - 1
	res.Changed = changed
	return res, nil
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
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
