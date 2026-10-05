package inventory

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
	mu         sync.Mutex
	maxAcct    int
	maxName    int
	maxAbs     int64
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAcct:  o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		accounts: make(map[string]Account),
	}, nil
}

func validName(l *Ledger, s string) bool {
	if s == "" || len(s) > l.maxName {
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

func (l *Ledger) checkAbs(v int64) bool {
	return v <= l.maxAbs && v >= -l.maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(l, op.Name) {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Candidate transaction: buffer writes for touched accounts only;
	// commit happens after all checks pass, so failure is a no-op.
	changedOrder := []string{}
	changed := make(map[string]Account)
	rev := l.revision
	pending := make(map[string]Account) // candidate state for touched accounts
	exists := make(map[string]bool)
	get := func(name string) (Account, bool) {
		if e, ok := exists[name]; ok {
			if !e {
				return Account{}, false
			}
			return pending[name], true
		}
		a, ok := l.accounts[name]
		if ok {
			pending[name] = a
			exists[name] = true
			return a, true
		}
		exists[name] = false
		return Account{}, false
	}
	for _, op := range b.Ops {
		cur, ok := get(op.Name)
		switch op.Kind {
		case Add:
			var nv int64
			if ok {
				// Overflow check before arithmetic.
				if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv = cur.Value + op.Delta
			} else {
				if !l.checkAbs(op.Delta) {
					return Result{}, ErrValue
				}
				nv = op.Delta
			}
			if !l.checkAbs(nv) {
				return Result{}, ErrValue
			}
			rev++
			pending[op.Name] = Account{Name: op.Name, Value: nv, Revision: rev}
			exists[op.Name] = true
		case Set:
			if !l.checkAbs(op.Value) {
				return Result{}, ErrValue
			}
			rev++
			pending[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: rev}
			exists[op.Name] = true
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			exists[op.Name] = false
			delete(pending, op.Name)
		}
		if _, seen := changed[op.Name]; !seen {
			changedOrder = append(changedOrder, op.Name)
		}
		// Track final state; deleted accounts drop out below.
		if exists[op.Name] {
			changed[op.Name] = pending[op.Name]
		} else {
			delete(changed, op.Name)
		}
	}

	// Final capacity check at batch end.
	finalCount := len(l.accounts)
	for name, e := range exists {
		_, was := l.accounts[name]
		if e && !was {
			finalCount++
		} else if !e && was {
			finalCount--
		}
	}
	if finalCount > l.maxAcct {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, e := range exists {
		if e {
			l.accounts[name] = pending[name]
		} else {
			delete(l.accounts, name)
		}
	}
	l.revision = rev
	l.generation++

	out := Result{Generation: l.generation, Revision: rev}
	for _, name := range changedOrder {
		if a, ok := changed[name]; ok {
			out.Changed = append(out.Changed, a)
		}
	}
	return out, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, 0, len(l.accounts))
	for name := range l.accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	s := Snapshot{Generation: l.generation, NextRevision: l.revision + 1}
	for _, name := range names {
		s.Accounts = append(s.Accounts, l.accounts[name])
	}
	return s
}
