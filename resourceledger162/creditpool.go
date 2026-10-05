package resourceledger162

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
	mu       sync.RWMutex
	opts     Options
	accounts map[string]Account
	// generation counts successful non-empty batches.
	generation uint64
	// revision is the last revision assigned to an Add/Set op.
	revision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
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

func checkAbs(v, maxAbs int64) error {
	if v > maxAbs || v < -maxAbs {
		return ErrValue
	}
	return nil
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

	// Candidate transaction: mutate clones, commit only on full success.
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	revision := l.revision
	var changed []Account
	touched := make(map[string]int) // name -> index in changed

	markChanged := func(a Account) {
		if i, ok := touched[a.Name]; ok {
			changed[i] = a
			return
		}
		touched[a.Name] = len(changed)
		changed = append(changed, a)
	}
	unmark := func(name string) {
		if i, ok := touched[name]; ok {
			changed = append(changed[:i], changed[i+1:]...)
			delete(touched, name)
			for j := i; j < len(changed); j++ {
				touched[changed[j].Name] = j
			}
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc := candidate[op.Name]
			// Overflow check before arithmetic.
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			nv := acc.Value + op.Delta
			if err := checkAbs(nv, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			revision++
			acc.Value = nv
			acc.Name = op.Name
			acc.Revision = revision
			candidate[op.Name] = acc
			markChanged(acc)
		case Set:
			if err := checkAbs(op.Value, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			revision++
			acc := Account{Name: op.Name, Value: op.Value, Revision: revision}
			candidate[op.Name] = acc
			markChanged(acc)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			unmark(op.Name)
		}
	}

	// Account capacity is only checked at the end of the batch.
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.revision = revision
	l.generation++
	return Result{Generation: l.generation, Revision: revision, Changed: changed}, nil
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
	return append([]Account(nil), all[:n]...), nil
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
