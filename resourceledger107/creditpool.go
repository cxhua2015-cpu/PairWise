package resourceledger107

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
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

func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func (l *Ledger) absLimit(v int64) bool {
	// Reject math.MinInt64 whose absolute value is not representable.
	return v != math.MinInt64 && (v < 0 && -v <= l.maxAbsValue || v >= 0 && v <= l.maxAbsValue)
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: apply ops on a clone; swap in only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if !l.absLimit(op.Delta) {
				return Result{}, ErrValue
			}
			a := cand[op.Name]
			if (op.Delta > 0 && a.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && a.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			a.Value += op.Delta
			if !l.absLimit(a.Value) {
				return Result{}, ErrValue
			}
			a.Name = op.Name
			a.Revision = rev
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			if !l.absLimit(op.Value) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	if len(cand) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRevision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
