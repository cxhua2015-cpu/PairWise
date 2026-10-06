package balanceledger267

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

// absLimit returns the absolute value of v and whether it fits in int64.
func absOK(v int64) (uint64, bool) {
	if v >= 0 {
		return uint64(v), true
	}
	if v == -1<<63 {
		return 0, false
	}
	return uint64(-v), true
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// Candidate transaction: work on a private copy, commit only on success.
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	revision := l.nextRevision
	var changed []Account
	seen := make(map[string]bool)

	record := func(name string) {
		if !seen[name] {
			seen[name] = true
			if acc, ok := cand[name]; ok {
				changed = append(changed, acc)
			}
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := cand[op.Name]
			if !ok {
				acc = Account{Name: op.Name}
			}
			d := op.Delta
			if a, fit := absOK(d); !fit || a > uint64(l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			// Overflow check before arithmetic.
			if (d > 0 && acc.Value > (1<<63-1)-d) || (d < 0 && acc.Value < (-1<<63)-d) {
				return Result{}, ErrValue
			}
			nv := acc.Value + d
			if a, fit := absOK(nv); !fit || a > uint64(l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Value = nv
			acc.Revision = revision
			revision++
			cand[op.Name] = acc
			record(op.Name)
		case Set:
			if a, fit := absOK(op.Value); !fit || a > uint64(l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc, ok := cand[op.Name]
			if !ok {
				acc = Account{Name: op.Name}
			}
			acc.Value = op.Value
			acc.Revision = revision
			revision++
			cand[op.Name] = acc
			record(op.Name)
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			record(op.Name)
		}
	}

	// Final capacity check only at batch end.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// Commit.
	l.accounts = cand
	l.nextRevision = revision
	l.generation++
	// Refresh changed entries to final values.
	for i, acc := range changed {
		changed[i] = cand[acc.Name]
	}
	return Result{Generation: l.generation, Revision: revision - 1, Changed: changed}, nil
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
