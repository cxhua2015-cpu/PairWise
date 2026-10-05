package resourceledger157

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
	mu         sync.Mutex
	maxAcct    int
	maxName    int
	maxAbs     int64
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAcct:  o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		maxAbs:   o.MaxAbsValue,
		accounts: make(map[string]Account),
	}, nil
}

func (l *Ledger) validName(n string) bool {
	if n == "" || len(n) > l.maxName {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (l *Ledger) withinAbs(v int64) bool {
	return v >= -l.maxAbs && v <= l.maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 阶段一：完整结构校验，不读取任何状态。
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !l.validName(op.Name) {
			return Result{}, ErrInvalidInput
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// 阶段二：候选事务，在克隆状态上按输入顺序执行。
	cand := make(map[string]Account, len(l.accounts))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
	changedIdx := make(map[string]int)
	var changed []Account
	note := func(a Account) {
		if i, ok := changedIdx[a.Name]; ok {
			changed[i] = a
		} else {
			changedIdx[a.Name] = len(changed)
			changed = append(changed, a)
		}
	}

	for _, op := range b.Ops {
		cur, ok := cand[op.Name]
		switch op.Kind {
		case Add:
			base := int64(0)
			if ok {
				base = cur.Value
			}
			if op.Delta > 0 && base > math.MaxInt64-op.Delta ||
				op.Delta < 0 && base < math.MinInt64-op.Delta {
				return Result{}, ErrValue
			}
			nv := base + op.Delta
			if !l.withinAbs(nv) {
				return Result{}, ErrValue
			}
			rev++
			a := Account{Name: op.Name, Value: nv, Revision: rev}
			cand[op.Name] = a
			note(a)
		case Set:
			if !l.withinAbs(op.Value) {
				return Result{}, ErrValue
			}
			rev++
			a := Account{Name: op.Name, Value: op.Value, Revision: rev}
			cand[op.Name] = a
			note(a)
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
		}
	}

	// 阶段三：批次末容量检查，失败整体回滚。
	if len(cand) > l.maxAcct {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.revision = rev
	l.generation++
	return Result{Generation: l.generation, Revision: rev, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
	l.mu.Lock()
	defer l.mu.Unlock()
	accs := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accs = append(accs, a)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accs,
	}
}
