package balanceledger427

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
	generation   uint64
	nextRevision uint64
	accounts     map[string]Account
}

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, nextRevision: 1, accounts: make(map[string]Account)}, nil
}

// applyLocked executes the batch against the locked ledger, rolling back on error.
func (l *Ledger) applyLocked(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	touched := make(map[string]accountBackup)
	changed := make([]Account, 0, len(b.Ops))
	changedIdx := make(map[string]int)

	record := func(name string) {
		if _, ok := touched[name]; !ok {
			acc, exists := l.accounts[name]
			touched[name] = accountBackup{acc, exists}
		}
	}
	note := func(acc Account) {
		if i, ok := changedIdx[acc.Name]; ok {
			changed[i] = acc
			return
		}
		changedIdx[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	rev := l.nextRevision
	for _, op := range b.Ops {
		record(op.Name)
		acc, exists := l.accounts[op.Name]
		switch op.Kind {
		case Add:
			v := acc.Value
			if (op.Delta > 0 && v > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && v < math.MinInt64-op.Delta) {
				rollback(l.accounts, touched)
				return Result{}, ErrValue
			}
			v += op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				rollback(l.accounts, touched)
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			l.accounts[op.Name] = acc
			note(acc)
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				rollback(l.accounts, touched)
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			l.accounts[op.Name] = acc
			note(acc)
		case Delete:
			if !exists {
				rollback(l.accounts, touched)
				return Result{}, ErrNotFound
			}
			delete(l.accounts, op.Name)
			note(acc)
		}
	}

	if len(l.accounts) > l.opts.MaxAccounts {
		rollback(l.accounts, touched)
		return Result{}, ErrCapacity
	}

	l.generation++
	l.nextRevision = rev
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

type accountBackup struct {
	acc    Account
	exists bool
}

func rollback(accounts map[string]Account, touched map[string]accountBackup) {
	for name, b := range touched {
		if b.exists {
			accounts[name] = b.acc
		} else {
			delete(accounts, name)
		}
	}
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.applyLocked(b)
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
	return accs[:n], nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() Snapshot {
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Accounts: accs}
}
