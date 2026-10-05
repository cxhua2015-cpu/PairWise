package resourceledger122

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

// validateOp performs structural validation only; it must not read state.
func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add:
		if op.Value != 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Delta != 0 {
			return ErrInvalidInput
		}
	case Delete:
		if op.Delta != 0 || op.Value != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	return nil
}

// exceedsLimit reports whether |v| > limit without overflowing on MinInt64.
func exceedsLimit(v, limit int64) bool {
	if v == math.MinInt64 {
		return true // |MinInt64| = 2^63 > any valid limit (<= MaxInt64)
	}
	if v < 0 {
		v = -v
	}
	return v > limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	type pending struct {
		acc     Account
		exists  bool
		deleted bool
	}
	work := make(map[string]*pending, len(b.Ops))
	get := func(name string) *pending {
		if p, ok := work[name]; ok {
			return p
		}
		acc, ok := l.accounts[name]
		p := &pending{acc: acc, exists: ok}
		work[name] = p
		return p
	}

	var order []string
	rev := l.revision
	for _, op := range b.Ops {
		p := get(op.Name)
		switch op.Kind {
		case Add:
			if exceedsLimit(op.Delta, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			if !p.exists || p.deleted {
				p.acc = Account{Name: op.Name, Value: op.Delta}
			} else {
				v := p.acc.Value
				if (op.Delta > 0 && v > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && v < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				p.acc.Value = v + op.Delta
			}
			if exceedsLimit(p.acc.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			rev++
			p.acc.Revision = rev
			p.exists, p.deleted = true, false
		case Set:
			if exceedsLimit(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			p.acc = Account{Name: op.Name, Value: op.Value}
			rev++
			p.acc.Revision = rev
			p.exists, p.deleted = true, false
		case Delete:
			if !p.exists || p.deleted {
				return Result{}, ErrNotFound
			}
			p.deleted = true
		}
		if len(order) == 0 || order[len(order)-1] != op.Name {
			seen := false
			for _, n := range order {
				if n == op.Name {
					seen = true
					break
				}
			}
			if !seen {
				order = append(order, op.Name)
			}
		}
	}

	// Final capacity check only at batch end.
	final := len(l.accounts)
	for name, p := range work {
		_, inLedger := l.accounts[name]
		switch {
		case p.deleted && inLedger:
			final--
		case !p.deleted && !inLedger:
			final++
		}
	}
	if final > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	changed := make([]Account, 0, len(order))
	for _, name := range order {
		p := work[name]
		if p.deleted {
			delete(l.accounts, name)
			continue
		}
		l.accounts[name] = p.acc
		changed = append(changed, p.acc)
	}
	l.revision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
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
	if n > len(accs) {
		n = len(accs)
	}
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     make([]Account, 0, len(l.accounts)),
	}
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}
