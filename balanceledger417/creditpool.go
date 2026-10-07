package balanceledger417

import "errors"
import (
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
	return &Ledger{
		opts:         o,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func addChecked(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}

func withinAbs(v, limit int64) bool {
	return v <= limit && v >= -limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := validateBatch(l.opts, b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}
	work := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		work[k] = v
	}
	rev := l.nextRevision
	changed := make([]Account, 0, len(b.Ops))
	changedIdx := make(map[string]int, len(b.Ops))
	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := work[op.Name]
			if ok {
				nv, ok := addChecked(acc.Value, op.Delta)
				if !ok {
					return Result{}, ErrValue
				}
				acc.Value = nv
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if !withinAbs(acc.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Revision = rev
			rev++
			work[op.Name] = acc
			record(acc)
		case Set:
			if !withinAbs(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			work[op.Name] = acc
			record(acc)
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
	l.nextRevision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
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
	return all[:n], nil
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
