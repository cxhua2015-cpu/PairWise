package resourceledger192

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
	mu       sync.RWMutex
	opts     Options
	accounts map[string]Account
	// generation counts successful non-empty batches.
	generation uint64
	// nextRevision is the revision the next Add/Set will assign.
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRevision: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateBatch performs full structural validation before any state is read.
func (l *Ledger) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func absLimit(v, limit int64) bool {
	// limit > 0; v == math.MinInt64 has no positive abs, reject.
	if v == -1<<63 {
		return false
	}
	if v < 0 {
		return -v <= limit
	}
	return v <= limit
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	if err := l.validateBatch(b); err != nil {
		return Result{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Candidate transaction: stage changes on a copy of touched entries.
	staged := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		staged[k] = v
	}
	rev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account
	for _, op := range b.Ops {
		acc, ok := staged[op.Name]
		switch op.Kind {
		case Add:
			base := acc.Value
			if !ok {
				base = 0
			}
			// Detect int64 overflow before arithmetic.
			if (op.Delta > 0 && base > (1<<63-1)-op.Delta) ||
				(op.Delta < 0 && base < (-1<<63)-op.Delta) {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if !absLimit(nv, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: nv, Revision: rev}
			rev++
			staged[op.Name] = acc
		case Set:
			if !absLimit(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: rev}
			rev++
			staged[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(staged, op.Name)
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j, a := range changed {
					changedIdx[a.Name] = j
				}
			}
			continue
		}
		if i, seen := changedIdx[op.Name]; seen {
			changed[i] = acc
		} else {
			changedIdx[op.Name] = len(changed)
			changed = append(changed, acc)
		}
	}
	// Final account capacity is checked only at batch end.
	if len(staged) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}
	// Commit.
	l.accounts = staged
	if len(b.Ops) > 0 {
		l.generation++
	}
	l.nextRevision = rev
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
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.nextRevision,
		Accounts:     accs,
	}
}
