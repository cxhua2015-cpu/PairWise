package balanceledger432

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

// Ledger is a concurrency-safe in-memory balance ledger.
type Ledger struct {
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, accounts: make(map[string]Account), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	work := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		work[k] = v
	}
	rev := l.nextRevision
	touched := make(map[string]bool, len(b.Ops))
	var order []string

	for _, op := range b.Ops {
		if !touched[op.Name] {
			touched[op.Name] = true
			order = append(order, op.Name)
		}
		switch op.Kind {
		case Add:
			acc := work[op.Name]
			nv, err := addChecked(acc.Value, op.Delta)
			if err != nil {
				return Result{}, err
			}
			if err := checkAbs(nv, l.opts.MaxAbsValue); err != nil {
				return Result{}, err
			}
			acc.Name = op.Name
			acc.Value = nv
			acc.Revision = rev
			rev++
			work[op.Name] = acc
		case Set:
			work[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
		case Delete:
			if _, ok := work[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(work, op.Name)
		}
	}
	if len(work) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = work
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = rev

	res := Result{Generation: l.generation, Revision: rev - 1}
	for _, name := range order {
		if acc, ok := work[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
	}
	return res, nil
}

// Top returns up to n accounts ordered by value descending, then name ascending.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return []Account{}, nil
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
	if n < len(accs) {
		accs = accs[:n]
	}
	return accs, nil
}

// Snapshot returns accounts sorted by name; the result is detached from internal state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision}
	s.Accounts = make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		s.Accounts = append(s.Accounts, a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].Name < s.Accounts[j].Name })
	return s
}

func addChecked(a, b int64) (int64, error) {
	if (b > 0 && a > 0 && a > maxInt64-b) || (b < 0 && a < 0 && a < minInt64-b) {
		return 0, ErrValue
	}
	return a + b, nil
}

func checkAbs(v, limit int64) error {
	if v > limit || v < -limit {
		return ErrValue
	}
	return nil
}

const maxInt64 = int64(^uint64(0) >> 1)
const minInt64 = -maxInt64 - 1
