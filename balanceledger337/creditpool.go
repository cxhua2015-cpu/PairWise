package balanceledger337

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	generation   uint64
	nextRevision uint64
	accounts     map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
		nextRevision: 1,
		accounts:     make(map[string]Account),
	}, nil
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

func absWithin(v, max int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= max
}

func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if b.Ops == nil {
		b.Ops = nil
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	touched := make(map[string]struct{}, len(b.Ops))
	next := l.nextRevision

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Add:
			if !absWithin(op.Delta, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			cur, ok := cand[op.Name]
			nv := op.Delta
			if ok {
				var good bool
				nv, good = addChecked(cur.Value, op.Delta)
				if !good {
					return Result{}, ErrValue
				}
			}
			if !absWithin(nv, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			cand[op.Name] = Account{Name: op.Name, Value: nv, Revision: next}
			next++
		case Set:
			if !absWithin(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: next}
			next++
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
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = next

	changed := make([]Account, 0, len(touched))
	for name := range touched {
		if acc, ok := cand[name]; ok {
			changed = append(changed, acc)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

	return Result{Generation: l.generation, Revision: next - 1, Changed: changed}, nil
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
	if n < len(accs) {
		accs = accs[:n]
	}
	out := make([]Account, len(accs))
	copy(out, accs)
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
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
