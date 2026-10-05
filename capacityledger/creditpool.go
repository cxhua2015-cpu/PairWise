package capacityledger

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
	maxAcct    int
	maxName    int
	maxAbs     int64
	generation uint64
	revision   uint64
	accounts   map[string]Account
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

func withinAbs(v, maxAbs int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= maxAbs
}

// validate checks the batch structurally without touching ledger state.
func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		if !validName(op.Name, l.maxName) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return ErrInvalidInput
			}
			if !withinAbs(op.Delta, l.maxAbs) {
				return ErrValue
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if !withinAbs(op.Value, l.maxAbs) {
				return ErrValue
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validate(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		l.mu.RLock()
		res := Result{Generation: l.generation, Revision: l.revision}
		l.mu.RUnlock()
		return res, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage mutations on a copy of touched state.
	staged := make(map[string]Account, len(b.Ops))
	prior := make(map[string]bool, len(b.Ops))
	after := make(map[string]bool, len(b.Ops))
	var order []string
	revision := l.revision

	stage := func(name string) (Account, bool) {
		if _, ok := staged[name]; !ok {
			acct, ok := l.accounts[name]
			staged[name] = acct
			prior[name] = ok
			after[name] = ok
			order = append(order, name)
		}
		return staged[name], after[name]
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acct, _ := stage(op.Name)
			if (op.Delta > 0 && acct.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acct.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			acct.Value += op.Delta
			if !withinAbs(acct.Value, l.maxAbs) {
				return Result{}, ErrValue
			}
			revision++
			acct.Name = op.Name
			acct.Revision = revision
			staged[op.Name] = acct
			after[op.Name] = true
		case Set:
			_, _ = stage(op.Name)
			revision++
			staged[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: revision}
			after[op.Name] = true
		case Delete:
			_, ok := stage(op.Name)
			if !ok {
				return Result{}, ErrNotFound
			}
			after[op.Name] = false
		}
	}

	// Final account-count capacity check at end of batch.
	final := len(l.accounts)
	for _, name := range order {
		if after[name] && !prior[name] {
			final++
		} else if !after[name] && prior[name] {
			final--
		}
	}
	if final > l.maxAcct {
		return Result{}, ErrCapacity
	}

	// Commit.
	changed := make([]Account, 0, len(order))
	for _, name := range order {
		if after[name] {
			acct := staged[name]
			l.accounts[name] = acct
			changed = append(changed, acct)
		} else {
			delete(l.accounts, name)
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
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
	out := make([]Account, n)
	copy(out, accts[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accts,
	}
}
