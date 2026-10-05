package resourceledger142

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
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	type staged struct {
		acc     Account
		exists  bool
		deleted bool
	}
	staging := make(map[string]*staged)
	get := func(name string) *staged {
		if s, ok := staging[name]; ok {
			return s
		}
		acc, exists := l.accounts[name]
		s := &staged{acc: acc, exists: exists}
		staging[name] = s
		return s
	}

	rev := l.nextRevision
	var changed []string
	seenChanged := make(map[string]bool)
	for _, op := range b.Ops {
		s := get(op.Name)
		switch op.Kind {
		case Add:
			base := int64(0)
			if s.exists && !s.deleted {
				base = s.acc.Value
			}
			if (op.Delta > 0 && base > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && base < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			v := base + op.Delta
			if v > l.maxAbsValue || v < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			s.acc = Account{Name: op.Name, Value: v, Revision: rev}
			s.exists = true
			s.deleted = false
			rev++
		case Set:
			if op.Value > l.maxAbsValue || op.Value < -l.maxAbsValue {
				return Result{}, ErrValue
			}
			s.acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			s.exists = true
			s.deleted = false
			rev++
		case Delete:
			if !s.exists || s.deleted {
				return Result{}, ErrNotFound
			}
			s.deleted = true
		}
		if !seenChanged[op.Name] {
			seenChanged[op.Name] = true
			changed = append(changed, op.Name)
		}
	}

	// Final account capacity checked only at batch end.
	final := len(l.accounts)
	for name, s := range staging {
		_, exists := l.accounts[name]
		switch {
		case s.deleted && exists:
			final--
		case !s.deleted && !exists:
			final++
		}
	}
	if final > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, s := range staging {
		if s.deleted {
			delete(l.accounts, name)
		} else {
			l.accounts[name] = s.acc
		}
	}
	l.nextRevision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}

	res := Result{Generation: l.generation, Revision: rev - 1}
	for _, name := range changed {
		if s := staging[name]; !s.deleted {
			res.Changed = append(res.Changed, s.acc)
		}
	}
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	l.mu.RUnlock()
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
