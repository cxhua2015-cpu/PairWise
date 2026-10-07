package balanceledger367

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
	mu         sync.RWMutex
	opts       Options
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account)}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 完整结构校验，先于任何状态读取。
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// 候选事务：在克隆状态上按输入顺序应用，全部成功才提交。
	candidate := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		candidate[k] = v
	}
	revision := l.revision
	changed := make(map[string]Account)
	order := make([]string, 0, len(b.Ops))

	for _, op := range b.Ops {
		acc, exists := candidate[op.Name]
		switch op.Kind {
		case Add:
			if exists {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				nv := acc.Value + op.Delta
				if nv > l.opts.MaxAbsValue || nv < -l.opts.MaxAbsValue {
					return Result{}, ErrValue
				}
				acc.Value = nv
			} else {
				if op.Delta > l.opts.MaxAbsValue || op.Delta < -l.opts.MaxAbsValue {
					return Result{}, ErrValue
				}
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			revision++
			acc.Revision = revision
			candidate[op.Name] = acc
			if _, ok := changed[op.Name]; !ok {
				order = append(order, op.Name)
			}
			changed[op.Name] = acc
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return Result{}, ErrValue
			}
			revision++
			acc = Account{Name: op.Name, Value: op.Value, Revision: revision}
			candidate[op.Name] = acc
			if _, ok := changed[op.Name]; !ok {
				order = append(order, op.Name)
			}
			changed[op.Name] = acc
		case Delete:
			if !exists {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			delete(changed, op.Name)
		}
	}

	// 最终账户容量仅在批次末检查。
	if len(candidate) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = candidate
	l.revision = revision
	l.generation++

	res := Result{Generation: l.generation, Revision: revision}
	for _, name := range order {
		if acc, ok := changed[name]; ok {
			res.Changed = append(res.Changed, acc)
		}
	}
	return res, nil
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
	if n < len(accs) {
		accs = accs[:n]
	}
	out := make([]Account, len(accs))
	copy(out, accs)
	return out, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.revision + 1}
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	s.Accounts = accs
	return s
}
