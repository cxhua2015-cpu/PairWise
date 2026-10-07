package balanceledger407

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

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, nextRevision: 1, accounts: make(map[string]Account)}, nil
}

// applyOps executes the candidate transaction against a private copy of the
// account index. It returns the final state of every touched account in order
// of first appearance, or the first failure encountered.
func (l *Ledger) applyOps(cand map[string]Account, ops []Op) ([]Account, uint64, error) {
	rev := l.nextRevision
	changed := make([]Account, 0, len(ops))
	seen := make(map[string]int, len(ops))
	record := func(a Account) {
		if i, ok := seen[a.Name]; ok {
			changed[i] = a
		} else {
			seen[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}
	for _, op := range ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			// Detect int64 overflow before performing any arithmetic.
			if (op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && cur.Value < math.MinInt64-op.Delta) {
				return nil, 0, ErrValue
			}
			v := cur.Value + op.Delta
			if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
				return nil, 0, ErrValue
			}
			a := Account{Name: op.Name, Value: v, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Set:
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			cand[op.Name] = a
			record(a)
		case Delete:
			if !ok {
				return nil, 0, ErrNotFound
			}
			delete(cand, op.Name)
			record(Account{Name: op.Name})
		}
	}
	return changed, rev, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	changed, rev, err := l.applyOps(cand, b.Ops)
	if err != nil {
		return Result{}, err
	}
	// Final account capacity is only checked at the end of the batch.
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = rev
	l.accounts = cand
	return Result{
		Generation: l.generation,
		Revision:   rev - 1,
		Changed:    changed,
	}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Name < out[j].Name
	})
	if n < len(out) {
		out = out[:n]
	}
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     out,
	}
}
