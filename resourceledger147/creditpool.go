package resourceledger147

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

func checkAbs(v, max int64) error {
	if v == math.MinInt64 {
		return ErrValue
	}
	if v > max || v < -max {
		return ErrValue
	}
	return nil
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

	// Candidate transaction: clone the index so failure is a cheap drop.
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	var changed []Account
	seen := make(map[string]int)

	record := func(a Account) {
		if i, ok := seen[a.Name]; ok {
			changed[i] = a
			return
		}
		seen[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			var base int64
			if ok {
				base = cur.Value
			}
			// Overflow detection before arithmetic.
			if (op.Delta > 0 && base > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && base < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if err := checkAbs(nv, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			if err := checkAbs(op.Value, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, ok2 := seen[op.Name]; ok2 {
				changed = append(changed[:i], changed[i+1:]...)
				delete(seen, op.Name)
				for j := i; j < len(changed); j++ {
					seen[changed[j].Name] = j
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
	l.nextRevision = rev
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Name < out[j].Name
	})
	if n < len(out) {
		out = out[:n]
	}
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: out}
}
