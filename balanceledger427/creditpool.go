package balanceledger427

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

// validateOp reports whether a single op is structurally valid.
func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add:
		if op.Delta == 0 {
			return ErrInvalidInput
		}
	case Set, Delete:
	default:
		return ErrInvalidInput
	}
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	return nil
}

func absLimitOK(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}

// apply executes the batch against m, mutating it. It assumes structural
// validation already passed. rev is the next revision counter; it returns the
// updated counter, the changed accounts in first-touch order, and the last
// assigned revision.
func (l *Ledger) apply(m map[string]Account, rev uint64, ops []Op) (uint64, []Account, uint64, error) {
	var changed []Account
	touched := make(map[string]int)
	last := rev - 1
	mark := func(a Account) {
		if i, ok := touched[a.Name]; ok {
			changed[i] = a
		} else {
			touched[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	for _, op := range ops {
		switch op.Kind {
		case Add:
			cur, ok := m[op.Name]
			var nv int64
			if ok {
				d := op.Delta
				if (d > 0 && cur.Value > math.MaxInt64-d) || (d < 0 && cur.Value < math.MinInt64-d) {
					return 0, nil, 0, ErrValue
				}
				nv = cur.Value + d
			} else {
				nv = op.Delta
			}
			if !absLimitOK(nv, l.opts.MaxAbsValue) {
				return 0, nil, 0, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			last = a.Revision
			m[op.Name] = a
			mark(a)
		case Set:
			if !absLimitOK(op.Value, l.opts.MaxAbsValue) {
				return 0, nil, 0, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			last = a.Revision
			m[op.Name] = a
			mark(a)
		case Delete:
			if _, ok := m[op.Name]; !ok {
				return 0, nil, 0, ErrNotFound
			}
			delete(m, op.Name)
			delete(touched, op.Name)
			for i, a := range changed {
				if a.Name == op.Name {
					changed = append(changed[:i], changed[i+1:]...)
					break
				}
			}
			// rebuild index after removal
			for i, a := range changed {
				touched[a.Name] = i
			}
		}
	}
	if len(m) > l.opts.MaxAccounts {
		return 0, nil, 0, ErrCapacity
	}
	return rev, changed, last, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		m[k] = v
	}
	rev, changed, last, err := l.apply(m, l.nextRevision, b.Ops)
	if err != nil {
		return Result{}, err
	}
	l.accounts = m
	l.nextRevision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}
	return Result{Generation: l.generation, Revision: last, Changed: changed}, nil
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
	return all[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
