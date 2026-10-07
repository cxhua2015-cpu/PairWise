package balanceledger437

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

type account struct {
	value    int64
	revision uint64
}

// Ledger is a concurrency-safe in-memory balance ledger.
type Ledger struct {
	mu           sync.RWMutex
	opts         Options
	accounts     map[string]account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]account), nextRevision: 1}, nil
}

// validateName reports whether n satisfies the structural name rules.
func (l *Ledger) validateName(n string) bool {
	if n == "" || len(n) > l.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateOp checks the structural rules of a single op without reading state.
func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add, Set, Delete:
	default:
		return ErrInvalidInput
	}
	if !l.validateName(op.Name) {
		return ErrInvalidInput
	}
	if op.Kind == Add && op.Delta == 0 {
		return ErrInvalidInput
	}
	return nil
}

// checkAbs enforces the absolute-value limit without overflowing.
func (l *Ledger) checkAbs(v int64) error {
	if v > l.opts.MaxAbsValue || v < -l.opts.MaxAbsValue {
		return ErrValue
	}
	return nil
}

// applyLocked executes the batch against the locked state, mutating it.
// Callers must have structurally validated the batch first.
func (l *Ledger) applyLocked(b Batch) Result {
	startRevision := l.nextRevision
	touched := make(map[string]int) // name -> index in changed
	var changed []Account

	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set:
			a := l.accounts[op.Name]
			a.revision = l.nextRevision
			l.nextRevision++
			if op.Kind == Add {
				a.value += op.Delta
			} else {
				a.value = op.Value
			}
			l.accounts[op.Name] = a
			if i, ok := touched[op.Name]; ok {
				changed[i] = Account{Name: op.Name, Value: a.value, Revision: a.revision}
			} else {
				touched[op.Name] = len(changed)
				changed = append(changed, Account{Name: op.Name, Value: a.value, Revision: a.revision})
			}
		case Delete:
			delete(l.accounts, op.Name)
		}
	}

	res := Result{Generation: l.generation, Revision: l.nextRevision - 1, Changed: changed}
	if len(b.Ops) > 0 {
		l.generation++
		res.Generation = l.generation
	}
	if l.nextRevision == startRevision {
		res.Revision = startRevision - 1
	}
	return res
}

// checkSemantics validates value/capacity rules against a prospective state
// without mutating the ledger. It simulates the batch on a scratch copy.
func (l *Ledger) checkSemantics(b Batch) error {
	vals := make(map[string]int64, len(l.accounts))
	for k, a := range l.accounts {
		vals[k] = a.value
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			v := vals[op.Name]
			d := op.Delta
			if (d > 0 && v > (1<<63-1)-d) || (d < 0 && v < (-1<<63)-d) {
				return ErrValue
			}
			v += d
			if err := l.checkAbs(v); err != nil {
				return err
			}
			vals[op.Name] = v
		case Set:
			if err := l.checkAbs(op.Value); err != nil {
				return err
			}
			vals[op.Name] = op.Value
		case Delete:
			if _, ok := vals[op.Name]; !ok {
				return ErrNotFound
			}
			delete(vals, op.Name)
		}
	}
	if len(vals) > l.opts.MaxAccounts {
		return ErrCapacity
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.checkSemantics(b); err != nil {
		return Result{}, err
	}
	return l.applyLocked(b), nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	accs := l.sortedAccountsLocked()
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

// sortedAccountsLocked returns accounts sorted by name; caller holds a lock.
func (l *Ledger) sortedAccountsLocked() []Account {
	accs := make([]Account, 0, len(l.accounts))
	for name, a := range l.accounts {
		accs = append(accs, Account{Name: name, Value: a.value, Revision: a.revision})
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return accs
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     l.sortedAccountsLocked(),
	}
}
