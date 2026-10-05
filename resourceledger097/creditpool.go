package resourceledger097

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
	mu           sync.Mutex
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

func absOK(v, maxAbs int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
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
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: apply onto a clone, commit by swapping.
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, exists := cand[op.Name]
		switch op.Kind {
		case Add:
			var nv int64
			if exists {
				if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if !absOK(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			record(a)
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}

	// Final capacity check only at batch end.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.generation++
	l.nextRevision = nextRev
	return Result{
		Generation: l.generation,
		Revision:   nextRev - 1,
		Changed:    append([]Account(nil), changed...),
	}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	return append([]Account(nil), all[:n]...), nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
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
