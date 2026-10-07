package balanceledger312

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
	mu       sync.Mutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRev: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

func absOK(v, limit int64) bool {
	if v == -1<<63 {
		return false
	}
	if v < 0 {
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
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}

	// Candidate transaction: clone current state, mutate, commit on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.nextRev
	var changed []Account
	touched := make(map[string]int) // name -> index in changed

	setChanged := func(a Account) {
		if i, ok := touched[a.Name]; ok {
			changed[i] = a
		} else {
			touched[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if !absOK(op.Delta, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a, ok := cand[op.Name]
			var nv int64
			if ok {
				// Overflow check before arithmetic.
				if (op.Delta > 0 && a.Value > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && a.Value < (-1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				nv = a.Value + op.Delta
			} else {
				nv = op.Delta
			}
			if !absOK(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a = Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			cand[op.Name] = a
			setChanged(a)
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			setChanged(a)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	// Final account capacity checked only at batch end.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.gen++
	l.nextRev = rev
	return Result{Generation: l.gen, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	as := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		as = append(as, a)
	}
	sort.Slice(as, func(i, j int) bool {
		if as[i].Value != as[j].Value {
			return as[i].Value > as[j].Value
		}
		return as[i].Name < as[j].Name
	})
	if n < len(as) {
		as = as[:n]
	}
	return as, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	as := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		as = append(as, a)
	}
	sort.Slice(as, func(i, j int) bool { return as[i].Name < as[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: as}
}
