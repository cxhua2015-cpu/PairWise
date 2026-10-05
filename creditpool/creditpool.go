package creditpool

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
	maxAccounts  int
	maxNameBytes int
	maxAbsValue  int64
	accounts     map[string]Account
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAccounts:  o.MaxAccounts,
		maxNameBytes: o.MaxNameBytes,
		maxAbsValue:  o.MaxAbsValue,
		accounts:     make(map[string]Account),
		nextRevision: 1,
	}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
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

func (l *Ledger) checkAbs(v int64) error {
	if v > l.maxAbsValue || v < -l.maxAbsValue {
		return ErrValue
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// 结构校验必须先于任何状态读取。
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxNameBytes) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.nextRevision - 1}, nil
	}

	// 候选事务：在副本上按输入顺序执行，失败则整体丢弃。
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRevision
	changedIdx := make(map[string]int)
	var changed []Account

	for _, op := range b.Ops {
		acc, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			if ok {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				acc.Value += op.Delta
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if err := l.checkAbs(acc.Value); err != nil {
				return Result{}, err
			}
			acc.Revision = nextRev
			nextRev++
			cand[op.Name] = acc
		case Set:
			if err := l.checkAbs(op.Value); err != nil {
				return Result{}, err
			}
			acc = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			cand[op.Name] = acc
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			if i, seen := changedIdx[op.Name]; seen {
				changed = append(changed[:i], changed[i+1:]...)
				delete(changedIdx, op.Name)
				for j := i; j < len(changed); j++ {
					changedIdx[changed[j].Name] = j
				}
			}
			continue
		}
		if i, seen := changedIdx[op.Name]; seen {
			changed[i] = cand[op.Name]
		} else {
			changedIdx[op.Name] = len(changed)
			changed = append(changed, cand[op.Name])
		}
	}

	// 账户容量仅在批次末检查。
	if len(cand) > l.maxAccounts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.generation++
	l.nextRevision = nextRev
	return Result{Generation: l.generation, Revision: nextRev - 1, Changed: changed}, nil
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
	return accs, nil
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
