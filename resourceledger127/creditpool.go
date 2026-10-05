package resourceledger127

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
	nextRev    uint64 // next revision to allocate, starts at 1
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
		// maxAbs <= MaxInt64, so -maxAbs is safe
		return v >= -maxAbs
	}
	return v <= maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if !absOK(op.Delta, l.maxAbs) {
				return Result{}, ErrValue
			}
		case Set:
			if !absOK(op.Value, l.maxAbs) {
				return Result{}, ErrValue
			}
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRev - 1}, nil
	}

	// Phase 2: execute against a candidate (copy-on-write) view.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	nextRev := l.nextRev
	var changed []Account
	changedIdx := make(map[string]int)

	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		cur, existed := candidate[op.Name]
		switch op.Kind {
		case Add:
			// Overflow check before arithmetic.
			if (op.Delta > 0 && cur.Value > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && cur.Value < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := cur.Value + op.Delta
			if !absOK(nv, l.maxAbs) {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			candidate[op.Name] = cur
			record(cur)
		case Set:
			cur = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = cur
			record(cur)
		case Delete:
			if !existed {
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
	if len(candidate) > l.maxAccts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = candidate
	l.nextRev = nextRev
	l.generation++
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool {
		if accts[i].Value != accts[j].Value {
			return accts[i].Value > accts[j].Value
		}
		return accts[i].Name < accts[j].Name
	})
	if n < len(accts) {
		accts = accts[:n]
	}
	return accts, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRev,
		Accounts:     accts,
	}
}
