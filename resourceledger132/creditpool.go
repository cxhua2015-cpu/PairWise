package resourceledger132

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
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func absLimit(v, max int64) bool { return v <= max && v >= -max }

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validate(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}
	next := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		next[k] = v
	}
	rev := l.nextRev
	changed := make([]Account, 0, len(b.Ops))
	seen := make(map[string]int, len(b.Ops))
	record := func(a Account) {
		if i, ok := seen[a.Name]; ok {
			changed[i] = a
			return
		}
		seen[a.Name] = len(changed)
		changed = append(changed, a)
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur := next[op.Name].Value
			if op.Delta > 0 && cur > math.MaxInt64-op.Delta ||
				op.Delta < 0 && cur < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			v := cur + op.Delta
			if !absLimit(v, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			next[op.Name] = a
			record(a)
		case Set:
			if !absLimit(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			next[op.Name] = a
			record(a)
		case Delete:
			a, ok := next[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
			record(a)
		}
	}
	if len(next) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	l.accounts = next
	l.gen++
	l.nextRev = rev
	return Result{Generation: l.gen, Revision: rev - 1, Changed: changed}, nil
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
	out := make([]Account, n)
	copy(out, all[:n])
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
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: accs}
}
