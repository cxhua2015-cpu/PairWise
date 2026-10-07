package balanceledger307

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
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRev: 1}, nil
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
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

func (l *Ledger) Apply(b Batch) (Result, error) {
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

	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	changed := make([]Account, 0, len(b.Ops))
	pos := make(map[string]int)
	rev := l.nextRev

	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			d := op.Delta
			if d > l.opts.MaxAbsValue || d < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			base := int64(0)
			if ok {
				base = cur.Value
			}
			if (d > 0 && base > math.MaxInt64-d) || (d < 0 && base < math.MinInt64-d) {
				return Result{}, ErrValue
			}
			nv := base + d
			if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			cand[op.Name] = a
			if i, seen := pos[op.Name]; seen {
				changed[i] = a
			} else {
				pos[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			if i, seen := pos[op.Name]; seen {
				changed[i] = a
			} else {
				pos[op.Name] = len(changed)
				changed = append(changed, a)
			}
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
		}
	}

	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		l.gen++
	}
	l.accounts = cand
	l.nextRev = rev
	out := make([]Account, len(changed))
	copy(out, changed)
	return Result{Generation: l.gen, Revision: rev - 1, Changed: out}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	all := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		all = append(all, a)
	}
	l.mu.RUnlock()
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
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: accts}
}
