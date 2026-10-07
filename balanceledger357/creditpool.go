package balanceledger357

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
	mu      sync.Mutex
	opts    Options
	accts   map[string]Account
	gen     uint64
	nextRev uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accts: make(map[string]Account), nextRev: 1}, nil
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

func absOK(v, limit int64) bool {
	// |v| <= limit, safe for math.MinInt64
	return v <= limit && v >= -limit
}

type undoEntry struct {
	name    string
	acct    Account
	existed bool
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Phase 1: full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	res := Result{Generation: l.gen, Revision: l.nextRev - 1}
	if len(b.Ops) == 0 {
		return res, nil
	}

	// Phase 2: candidate transaction with rollback journal.
	var journal []undoEntry
	changedIdx := make(map[string]int)
	var changed []Account
	rev := l.nextRev
	fail := func(err error) (Result, error) {
		for i := len(journal) - 1; i >= 0; i-- {
			u := journal[i]
			if u.existed {
				l.accts[u.name] = u.acct
			} else {
				delete(l.accts, u.name)
			}
		}
		return Result{}, err
	}
	record := func(name string) {
		a, ok := l.accts[name]
		journal = append(journal, undoEntry{name, a, ok})
	}
	markChanged := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	unmark := func(name string) {
		if i, ok := changedIdx[name]; ok {
			delete(changedIdx, name)
			changed = append(changed[:i], changed[i+1:]...)
			for j := i; j < len(changed); j++ {
				changedIdx[changed[j].Name] = j
			}
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			cur, ok := l.accts[op.Name]
			if !ok {
				cur = Account{Name: op.Name}
			}
			nv := cur.Value + op.Delta
			if (op.Delta > 0 && nv < cur.Value) || (op.Delta < 0 && nv > cur.Value) {
				return fail(ErrValue)
			}
			if !absOK(nv, l.opts.MaxAbsValue) {
				return fail(ErrValue)
			}
			record(op.Name)
			cur.Value = nv
			cur.Revision = rev
			rev++
			l.accts[op.Name] = cur
			markChanged(cur)
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return fail(ErrValue)
			}
			record(op.Name)
			cur := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			l.accts[op.Name] = cur
			markChanged(cur)
		case Delete:
			if _, ok := l.accts[op.Name]; !ok {
				return fail(ErrNotFound)
			}
			record(op.Name)
			delete(l.accts, op.Name)
			unmark(op.Name)
		}
	}

	// Final capacity check only at batch end.
	if len(l.accts) > l.opts.MaxAccounts {
		return fail(ErrCapacity)
	}

	l.nextRev = rev
	l.gen++
	res.Generation = l.gen
	res.Revision = rev - 1
	res.Changed = changed
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	accts := make([]Account, 0, len(l.accts))
	for _, a := range l.accts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool {
		if accts[i].Value != accts[j].Value {
			return accts[i].Value > accts[j].Value
		}
		return accts[i].Name < accts[j].Name
	})
	if n > len(accts) {
		n = len(accts)
	}
	return accts[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.gen, NextRevision: l.nextRev}
	if len(l.accts) > 0 {
		s.Accounts = make([]Account, 0, len(l.accts))
		for _, a := range l.accts {
			s.Accounts = append(s.Accounts, a)
		}
		sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	}
	return s
}
