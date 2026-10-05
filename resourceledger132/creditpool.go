package resourceledger132

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

func absLimit(v, limit int64) bool {
	// limit > 0; v == math.MinInt64 has no representable abs, treat as over limit.
	if v < 0 {
		if v == -v { // MinInt64
			return false
		}
		v = -v
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if !absLimit(op.Delta, l.maxAbsValue) {
				return Result{}, ErrValue
			}
		case Set:
			if !absLimit(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: work on a copy so failure rolls back for free.
	candidate := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRevision
	var changed []Account
	changedIdx := make(map[string]int)
	markChanged := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}

	for _, op := range b.Ops {
		cur, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			// Overflow check before arithmetic.
			if (op.Delta > 0 && cur.Value > 0 && cur.Value > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && cur.Value < 0 && cur.Value < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := cur.Value + op.Delta
			if !absLimit(nv, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			cur.Name = op.Name
			cur.Value = nv
			cur.Revision = nextRev
			nextRev++
			candidate[op.Name] = cur
			markChanged(cur)
		case Set:
			cur.Name = op.Name
			cur.Value = op.Value
			cur.Revision = nextRev
			nextRev++
			candidate[op.Name] = cur
			markChanged(cur)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
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
	if len(candidate) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	return Result{
		Generation: l.generation,
		Revision:   nextRev - 1,
		Changed:    changed,
	}, nil
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
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
