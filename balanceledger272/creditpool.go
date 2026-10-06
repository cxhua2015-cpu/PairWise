package balanceledger272

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
	generation uint64
	revision   uint64
	accounts   map[string]Account
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
}

// validName reports whether n satisfies the structural name rules.
func validName(n string, maxBytes int) bool {
	if n == "" || len(n) > maxBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp checks the structural semantics shared by Apply and ValidateBatch.
func (l *Ledger) validateOp(op Op) error {
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Add:
		if op.Delta == 0 || op.Value != 0 {
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
	return nil
}

// absWithin reports whether |v| <= limit without overflowing on MinInt64.
func absWithin(v, limit int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// Candidate transaction: stage all mutations on a private copy so a
	// failure anywhere leaves the committed state untouched.
	staged := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		staged[k] = v
	}
	revision := l.revision
	changed := make([]Account, 0, len(b.Ops))
	touched := make(map[string]int, len(b.Ops))

	record := func(acc Account) {
		if i, ok := touched[acc.Name]; ok {
			changed[i] = acc
			return
		}
		touched[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	for _, op := range b.Ops {
		acc, ok := staged[op.Name]
		switch op.Kind {
		case Add:
			base := acc.Value
			if !ok {
				base = 0
			}
			d := op.Delta
			if d > 0 && base > math.MaxInt64-d || d < 0 && base < math.MinInt64-d {
				return Result{}, ErrValue
			}
			nv := base + d
			if !absWithin(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			revision++
			acc = Account{Name: op.Name, Value: nv, Revision: revision}
			staged[op.Name] = acc
			record(acc)
		case Set:
			if !absWithin(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			revision++
			acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			staged[op.Name] = acc
			record(acc)
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(staged, op.Name)
			record(acc)
		}
	}

	if len(staged) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = staged
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
	if n < len(accs) {
		accs = accs[:n]
	}
	return accs, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.revision + 1, Accounts: accs}
}
