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

// state is the mutable ledger core. It is never shared across Ledgers;
// each Ledger owns its state exclusively behind its mutex.
type state struct {
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func newState() state {
	return state{accounts: make(map[string]Account), nextRevision: 1}
}

func (s state) copy() state {
	out := state{
		accounts:     make(map[string]Account, len(s.accounts)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for name, acc := range s.accounts {
		out.accounts[name] = acc
	}
	return out
}

// execute applies a structurally valid batch atomically to s.
// On error s is left unmodified.
func (s *state) execute(b Batch, opts Options) (Result, error) {
	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRevision - 1}, nil
	}
	work := s.copy()
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
		cur, exists := work.accounts[op.Name]
		switch op.Kind {
		case Add:
			if op.Delta > 0 && cur.Value > (1<<63-1)-op.Delta {
				return Result{}, ErrValue
			}
			if op.Delta < 0 && cur.Value < (-1<<63)-op.Delta {
				return Result{}, ErrValue
			}
			next := cur.Value + op.Delta
			if next > opts.MaxAbsValue || next < -opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			acc := Account{Name: op.Name, Value: next, Revision: work.nextRevision}
			work.nextRevision++
			work.accounts[op.Name] = acc
			record(acc)
		case Set:
			acc := Account{Name: op.Name, Value: op.Value, Revision: work.nextRevision}
			work.nextRevision++
			work.accounts[op.Name] = acc
			record(acc)
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(work.accounts, op.Name)
			record(cur)
		}
	}
	if len(work.accounts) > opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	work.generation++
	*s = work
	return Result{
		Generation: work.generation,
		Revision:   work.nextRevision - 1,
		Changed:    changed,
	}, nil
}

func (s state) snapshot() Snapshot {
	accs := make([]Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Accounts:     accs,
	}
}

// Ledger is a concurrency-safe in-memory balance ledger.
// The zero value is not usable; construct with New.
type Ledger struct {
	mu    sync.Mutex
	opts  Options
	state state
}

func New(opts Options) (*Ledger, error) {
	if opts.MaxAccounts <= 0 || opts.MaxNameBytes <= 0 || opts.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, state: newState()}, nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state.execute(b, l.opts)
}

func (l *Ledger) Top(n int) ([]Account, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.state.accounts))
	for _, acc := range l.state.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool {
		if accs[i].Value != accs[j].Value {
			return accs[i].Value > accs[j].Value
		}
		return accs[i].Name < accs[j].Name
	})
	if n < len(accs) {
		if n < 0 {
			n = 0
		}
		accs = accs[:n]
	}
	return accs, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state.snapshot()
}
