package balanceledger217

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
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
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

func (l *Ledger) checkAbs(v int64) bool {
	return v <= l.opts.MaxAbsValue && v >= -l.opts.MaxAbsValue
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
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
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Candidate transaction: mutate only copies of touched entries.
	type delta struct {
		acc     Account
		exists  bool // exists in candidate (not deleted)
		existed bool // existed in committed state before the batch
	}
	cand := make(map[string]*delta)
	var order []string // first-touch order of Add/Set accounts
	rev := l.revision

	get := func(name string) *delta {
		if d, ok := cand[name]; ok {
			return d
		}
		acc, ok := l.accounts[name]
		d := &delta{acc: acc, exists: ok, existed: ok}
		cand[name] = d
		return d
	}

	for _, op := range b.Ops {
		d := get(op.Name)
		switch op.Kind {
		case Add:
			var v int64
			if d.exists {
				cur := d.acc.Value
				// Detect overflow before/while computing cur+op.Delta.
				if (op.Delta > 0 && cur > (1<<63-1)-op.Delta) ||
					(op.Delta < 0 && cur < (-1<<63)-op.Delta) {
					return Result{}, ErrValue
				}
				v = cur + op.Delta
			} else {
				v = op.Delta
			}
			if !l.checkAbs(v) {
				return Result{}, ErrValue
			}
			rev++
			if !touched(order, op.Name) {
				order = append(order, op.Name)
			}
			d.acc = Account{Name: op.Name, Value: v, Revision: rev}
			d.exists = true
		case Set:
			if !l.checkAbs(op.Value) {
				return Result{}, ErrValue
			}
			rev++
			if !touched(order, op.Name) {
				order = append(order, op.Name)
			}
			d.acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			d.exists = true
		case Delete:
			if !d.exists {
				return Result{}, ErrNotFound
			}
			d.exists = false
		}
	}

	// Final account capacity is checked only at the end of the batch.
	final := len(l.accounts)
	for _, d := range cand {
		switch {
		case d.exists && !d.existed:
			final++
		case !d.exists && d.existed:
			final--
		}
	}
	if final > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for name, d := range cand {
		if d.exists {
			l.accounts[name] = d.acc
		} else if d.existed {
			delete(l.accounts, name)
		}
	}
	l.revision = rev
	l.generation++

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if d := cand[name]; d.exists {
			changed = append(changed, d.acc)
		}
	}
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
}

func touched(order []string, name string) bool {
	for _, n := range order {
		if n == name {
			return true
		}
	}
	return false
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
	out := make([]Account, n)
	copy(out, accs[:n])
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
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accs,
	}
}
