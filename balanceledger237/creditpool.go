package balanceledger237

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
// Ledger is a concurrency-safe in-memory balance ledger.
// All public methods may be called concurrently.
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

// Apply executes a batch atomically in input order. On any error the
// ledger is left untouched and the whole batch is rolled back.
func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1}
	if len(b.Ops) == 0 {
		return res, nil
	}

	// Candidate transaction: mutate a private copy so any failure
	// leaves the committed state untouched.
	candidate := make(map[string]Account, len(l.accounts))
	for name, acc := range l.accounts {
		candidate[name] = acc
	}
	nextRev := l.nextRevision
	changedIdx := make(map[string]int)
	changed := make([]Account, 0, len(b.Ops))

	record := func(acc Account) {
		if i, ok := changedIdx[acc.Name]; ok {
			changed[i] = acc
			return
		}
		changedIdx[acc.Name] = len(changed)
		changed = append(changed, acc)
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc := candidate[op.Name]
			if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
				(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
				return Result{}, ErrValue
			}
			v := acc.Value + op.Delta
			if absExceeds(v, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Name = op.Name
			acc.Value = v
			acc.Revision = nextRev
			nextRev++
			candidate[op.Name] = acc
			record(acc)
		case Set:
			if absExceeds(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			candidate[op.Name] = acc
			record(acc)
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.generation++
	l.nextRevision = nextRev
	res.Generation = l.generation
	res.Revision = nextRev - 1
	res.Changed = changed
	return res, nil
}

// Top returns up to n accounts ordered by value descending, then name
// ascending. The returned slice is detached from internal state.
func (l *Ledger) Top(n int) ([]Account, error) {
	if n <= 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
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

// Snapshot returns a consistent view sorted by account name. The
// returned slice is detached from internal state.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}

// absExceeds reports whether |v| > limit, safe for math.MinInt64.
func absExceeds(v, limit int64) bool {
	if v == math.MinInt64 {
		return true
	}
	if v < 0 {
		v = -v
	}
	return v > limit
}
