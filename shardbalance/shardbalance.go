package shardbalance

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

func absOK(v, max int64) bool {
	if v < 0 {
		if v == -1<<63 {
			return false
		}
		v = -v
	}
	return v <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
	touched := make(map[string]bool)
	var order []string

	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set:
			acc, ok := cand[op.Name]
			var nv int64
			if op.Kind == Add {
				if ok && ((op.Delta > 0 && acc.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && acc.Value < (-1<<63)-op.Delta)) {
					return Result{}, ErrValue
				}
				nv = acc.Value + op.Delta
			} else {
				nv = op.Value
			}
			if !absOK(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			rev++
			acc.Name = op.Name
			acc.Value = nv
			acc.Revision = rev
			cand[op.Name] = acc
			if !touched[op.Name] {
				touched[op.Name] = true
				order = append(order, op.Name)
			}
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.generation++
	}
	l.revision = rev
	l.accounts = cand

	var changed []Account
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
