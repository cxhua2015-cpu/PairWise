package resourceledger112

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
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
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

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, accounts: make(map[string]Account)}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
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

	// Candidate transaction: stage changes on a copy of the touched state.
	type staged struct {
		acc     Account
		exists  bool
		deleted bool
	}
	staging := make(map[string]*staged)
	order := []string{}
	get := func(name string) *staged {
		if s, ok := staging[name]; ok {
			return s
		}
		acc, exists := l.accounts[name]
		s := &staged{acc: acc, exists: exists}
		staging[name] = s
		order = append(order, name)
		return s
	}

	revision := l.revision
	changed := []Account{}
	changedIdx := make(map[string]int)
	count := len(l.accounts)

	for _, op := range b.Ops {
		s := get(op.Name)
		switch op.Kind {
		case Add:
			if s.deleted {
				s.deleted = false
				s.exists = true
				s.acc = Account{Name: op.Name}
				count++
			}
			if !s.exists {
				s.exists = true
				s.acc = Account{Name: op.Name}
				count++
			}
			// Overflow check before arithmetic.
			if (op.Delta > 0 && s.acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && s.acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			v := s.acc.Value + op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			s.acc.Value = v
			s.acc.Revision = revision
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			if s.deleted {
				s.deleted = false
				s.exists = true
				count++
			}
			if !s.exists {
				s.exists = true
				count++
			}
			revision++
			s.acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
		case Delete:
			if !s.exists || s.deleted {
				return Result{}, ErrNotFound
			}
			s.deleted = true
			count--
			if idx, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:idx], changed[idx+1:]...)
				delete(changedIdx, op.Name)
				for i, a := range changed {
					changedIdx[a.Name] = i
				}
			}
			continue
		}
		if idx, ok := changedIdx[op.Name]; ok {
			changed[idx] = s.acc
		} else {
			changedIdx[op.Name] = len(changed)
			changed = append(changed, s.acc)
		}
	}

	// Final capacity check only at batch end.
	if count > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	for _, name := range order {
		s := staging[name]
		if s.deleted {
			delete(l.accounts, name)
		} else if s.exists {
			l.accounts[name] = s.acc
		}
	}
	l.revision = revision
	l.generation++

	return Result{Generation: l.generation, Revision: revision, Changed: changed}, nil
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
