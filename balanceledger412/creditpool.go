package balanceledger412

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

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage mutations on a private copy so a
	// failure anywhere leaves the committed state untouched.
	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	revision := l.nextRevision
	var changed []Account
	touched := make(map[string]int, len(b.Ops))

	record := func(acc Account) {
		if i, ok := touched[acc.Name]; ok {
			changed[i] = acc
			return
		}
		touched[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta > l.opts.MaxAbsValue || op.Delta < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			base := int64(0)
			if exists {
				base = acc.Value
			}
			if (op.Delta > 0 && base > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && base < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			value := base + op.Delta
			if value > l.opts.MaxAbsValue || value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: value, Revision: revision}
			revision++
			candidate[op.Name] = acc
			record(acc)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			revision++
			candidate[op.Name] = acc
			record(acc)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			if i, ok := touched[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(touched, op.Name)
				for name, idx := range touched {
					if idx > i {
						touched[name] = idx - 1
					}
				}
			}
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = revision
	res := Result{Generation: l.generation, Changed: changed}
	if revision > 1 {
		res.Revision = revision - 1
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
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
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
