package resourceledger157

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

func exists(m map[string]Account, name string) bool {
	_, ok := m[name]
	return ok
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

func absLimit(v, limit int64) bool {
	// limit > 0; v == math.MinInt64 has no positive abs, treat as over limit.
	if v < 0 {
		if v == -v { // MinInt64
			return false
		}
		return -v <= limit
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: stage changes on copies, commit at the end.
	type staged struct {
		acc     Account
		present bool
	}
	staging := make(map[string]*staged)
	var order []string
	get := func(name string) *staged {
		if s, ok := staging[name]; ok {
			return s
		}
		acc, exists := l.accounts[name]
		s := &staged{acc: acc, present: exists}
		staging[name] = s
		order = append(order, name)
		return s
	}

	revision := l.nextRevision
	for _, op := range b.Ops {
		s := get(op.Name)
		switch op.Kind {
		case Add:
			cur := int64(0)
			if s.present {
				cur = s.acc.Value
			}
			// Overflow check before arithmetic.
			if (op.Delta > 0 && cur > 0 && op.Delta > 0x7fffffffffffffff-cur) ||
				(op.Delta < 0 && cur < 0 && op.Delta < -0x8000000000000000-cur) {
				return Result{}, ErrValue
			}
			v := cur + op.Delta
			if !absLimit(v, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			s.acc = Account{Name: op.Name, Value: v, Revision: revision}
			s.present = true
			revision++
		case Set:
			if !absLimit(op.Value, l.maxAbsValue) {
				return Result{}, ErrValue
			}
			s.acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			s.present = true
			revision++
		case Delete:
			if !s.present {
				return Result{}, ErrNotFound
			}
			s.present = false
		}
	}

	// Final account capacity checked only at batch end.
	final := len(l.accounts)
	for _, name := range order {
		s := staging[name]
		switch {
		case s.present && !exists(l.accounts, name):
			final++
		case !s.present && exists(l.accounts, name):
			final--
		}
	}
	if final > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	changed := make([]Account, 0, len(order))
	for _, name := range order {
		s := staging[name]
		if !s.present {
			delete(l.accounts, name)
		} else {
			l.accounts[name] = s.acc
			changed = append(changed, s.acc)
		}
	}
	l.generation++
	l.nextRevision = revision
	return Result{Generation: l.generation, Revision: revision - 1, Changed: changed}, nil
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
	if n < len(accs) {
		accs = accs[:n]
	}
	return accs, nil
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
