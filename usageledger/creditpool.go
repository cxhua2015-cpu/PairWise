package usageledger

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
	mu           sync.Mutex
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
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

func (l *Ledger) Apply(b Batch) (Result, error) {
	if b.Ops == nil {
		b.Ops = nil
	}
	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		l.mu.Lock()
		g := l.generation
		l.mu.Unlock()
		return Result{Generation: g}, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 2: candidate transaction on a cloned map.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRevision
	var lastRev uint64
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
			if !absOK(op.Delta, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			cur, ok := cand[op.Name]
			var nv int64
			if ok {
				// Detect overflow before performing arithmetic.
				if (op.Delta > 0 && cur.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && cur.Value < (-1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if !absOK(nv, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: nextRev}
			nextRev++
			lastRev = a.Revision
			cand[op.Name] = a
			record(a)
		case Set:
			if !absOK(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			lastRev = a.Revision
			cand[op.Name] = a
			record(a)
		case Delete:
			cur, ok := cand[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			record(cur)
		}
	}

	// Final capacity check only at batch end.
	if len(cand) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.nextRevision = nextRev
	l.generation++
	return Result{Generation: l.generation, Revision: lastRev, Changed: changed}, nil
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
	out := make([]Account, n)
	copy(out, accs[:n])
	return out, nil
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
