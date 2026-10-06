package balanceledger277

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
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRevision: 1}, nil
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

// checkAbs reports whether |v| <= max without overflowing.
func checkAbs(v, max int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= max
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	staged := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		staged[k] = v
	}
	nextRev := l.nextRevision
	var order []string
	final := make(map[string]Account)

	record := func(name string, acc Account) {
		if _, ok := final[name]; !ok {
			order = append(order, name)
		}
		final[name] = acc
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := staged[op.Name]
			if !ok {
				if !checkAbs(op.Delta, l.opts.MaxAbsValue) {
					return Result{}, ErrValue
				}
				acc = Account{Name: op.Name, Value: op.Delta, Revision: nextRev}
			} else {
				v, d := acc.Value, op.Delta
				if (d > 0 && v > math.MaxInt64-d) || (d < 0 && v < math.MinInt64-d) {
					return Result{}, ErrValue
				}
				nv := v + d
				if !checkAbs(nv, l.opts.MaxAbsValue) {
					return Result{}, ErrValue
				}
				acc.Value = nv
				acc.Revision = nextRev
			}
			nextRev++
			staged[op.Name] = acc
			record(op.Name, acc)
		case Set:
			if !checkAbs(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			staged[op.Name] = acc
			record(op.Name, acc)
		case Delete:
			acc, ok := staged[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(staged, op.Name)
			record(op.Name, acc)
		}
	}

	if len(staged) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = staged
	l.nextRevision = nextRev
	if len(b.Ops) > 0 {
		l.generation++
	}
	res := Result{
		Generation: l.generation,
		Revision:   l.nextRevision - 1,
		Changed:    make([]Account, 0, len(order)),
	}
	for _, name := range order {
		res.Changed = append(res.Changed, final[name])
	}
	return res, nil
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
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
