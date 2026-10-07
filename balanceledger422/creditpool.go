package balanceledger422

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

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, accounts: make(map[string]Account), nextRevision: 1}, nil
}

// apply executes the batch against the given state. It is the single
// transaction engine shared by Apply and Preview.
func applyBatch(opts Options, accounts map[string]Account, generation, nextRevision uint64, b Batch) (Result, map[string]Account, uint64, uint64, error) {
	if err := validateBatch(opts, b); err != nil {
		return Result{}, nil, 0, 0, err
	}
	work := make(map[string]Account, len(accounts)+len(b.Ops))
	for k, v := range accounts {
		work[k] = v
	}
	var changed []Account
	changedIdx := make(map[string]int)
	rev := nextRevision
	for _, op := range b.Ops {
		cur, ok := work[op.Name]
		switch op.Kind {
		case Add:
			if (op.Delta > 0 && cur.Value > maxInt64-op.Delta) || (op.Delta < 0 && cur.Value < minInt64-op.Delta) {
				return Result{}, nil, 0, 0, ErrValue
			}
			nv := cur.Value + op.Delta
			if !withinAbs(nv, opts.MaxAbsValue) {
				return Result{}, nil, 0, 0, ErrValue
			}
			cur = Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
		case Set:
			if !withinAbs(op.Value, opts.MaxAbsValue) {
				return Result{}, nil, 0, 0, ErrValue
			}
			cur = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
		case Delete:
			if !ok {
				return Result{}, nil, 0, 0, ErrNotFound
			}
			delete(work, op.Name)
			if i, hit := changedIdx[op.Name]; hit {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
			continue
		}
		work[op.Name] = cur
		if i, hit := changedIdx[op.Name]; hit {
			changed[i] = cur
		} else {
			changedIdx[op.Name] = len(changed)
			changed = append(changed, cur)
		}
	}
	if len(work) > opts.MaxAccounts {
		return Result{}, nil, 0, 0, ErrCapacity
	}
	gen := generation
	if len(b.Ops) > 0 {
		gen++
	}
	return Result{Generation: gen, Revision: rev - 1, Changed: changed}, work, gen, rev, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, work, gen, rev, err := applyBatch(l.opts, l.accounts, l.generation, l.nextRevision, b)
	if err != nil {
		return Result{}, err
	}
	l.accounts = work
	l.generation = gen
	l.nextRevision = rev
	return res, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if n <= 0 {
		return nil, nil
	}
	all := sortedAccounts(l.accounts)
	sort.Slice(all, func(i, j int) bool {
		if all[i].Value != all[j].Value {
			return all[i].Value > all[j].Value
		}
		return all[i].Name < all[j].Name
	})
	if n > len(all) {
		n = len(all)
	}
	return append([]Account(nil), all[:n]...), nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return snapshotOf(l.accounts, l.generation, l.nextRevision)
}

func sortedAccounts(m map[string]Account) []Account {
	all := make([]Account, 0, len(m))
	for _, a := range m {
		all = append(all, a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

func snapshotOf(m map[string]Account, generation, nextRevision uint64) Snapshot {
	return Snapshot{Generation: generation, NextRevision: nextRevision, Accounts: sortedAccounts(m)}
}

const maxInt64 = int64(^uint64(0) >> 1)
const minInt64 = -maxInt64 - 1

func withinAbs(v, limit int64) bool {
	if v == minInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}
