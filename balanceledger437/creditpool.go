package balanceledger437

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

type state struct {
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

type Ledger struct {
	mu   sync.RWMutex
	opts Options
	st   state
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, st: state{accounts: make(map[string]Account), nextRevision: 1}}, nil
}

// applyBatch executes the batch atomically against st. The caller must hold
// exclusive ownership of st. Structural validation runs before any state read.
func applyBatch(opts Options, st *state, b Batch) (Result, error) {
	if err := validateBatch(opts, b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}
	next := make(map[string]Account, len(st.accounts))
	for k, v := range st.accounts {
		next[k] = v
	}
	generation := st.generation
	nextRevision := st.nextRevision
	var changed []Account
	changedIdx := make(map[string]int)
	record := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
			return
		}
		changedIdx[a.Name] = len(changed)
		changed = append(changed, a)
	}
	for _, op := range b.Ops {
		cur, exists := next[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta > 0 && cur.Value > math.MaxInt64-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && cur.Value < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			v := cur.Value + op.Delta
			if v > opts.MaxAbsValue || v < -opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: v, Revision: nextRevision}
			nextRevision++
			next[op.Name] = cur
			record(cur)
		case Set:
			if op.Value > opts.MaxAbsValue || op.Value < -opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			cur = Account{Name: op.Name, Value: op.Value, Revision: nextRevision}
			nextRevision++
			next[op.Name] = cur
			record(cur)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(next, op.Name)
			if i, ok := changedIdx[op.Name]; ok {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
		}
	}
	if len(next) > opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	generation++
	st.accounts = next
	st.generation = generation
	st.nextRevision = nextRevision
	return Result{Generation: generation, Revision: nextRevision - 1, Changed: changed}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return applyBatch(l.opts, &l.st, b)
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := sortedAccounts(&l.st)
	if n > len(accs) {
		n = len(accs)
	}
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].Value != accs[j].Value {
			return accs[i].Value > accs[j].Value
		}
		return accs[i].Name < accs[j].Name
	})
	out := make([]Account, n)
	copy(out, accs[:n])
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return snapshotOf(&l.st)
}

func sortedAccounts(st *state) []Account {
	accs := make([]Account, 0, len(st.accounts))
	for _, a := range st.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return accs
}

func snapshotOf(st *state) Snapshot {
	return Snapshot{Generation: st.generation, NextRevision: st.nextRevision, Accounts: sortedAccounts(st)}
}
