package rewardledger

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

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

func (l *Ledger) checkValue(v int64) error {
	if v > l.maxAbs || v < -l.maxAbs {
		return ErrValue
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// 完整结构校验，先于任何状态读取。
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, l.maxName) {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.generation, Revision: l.revision}, nil
	}

	// 候选事务：在副本上执行，失败即丢弃，实现整体回滚。
	type pending struct {
		acc   Account
		seen  bool
		order int
	}
	cand := make(map[string]pending, len(b.Ops))
	var order []string
	revision := l.revision

	get := func(name string) (Account, bool) {
		if p, ok := cand[name]; ok {
			return p.acc, p.seen
		}
		a, ok := l.accounts[name]
		return a, ok
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := get(op.Name)
			if ok {
				if (op.Delta > 0 && acc.Value > math.MaxInt64-op.Delta) ||
					(op.Delta < 0 && acc.Value < math.MinInt64-op.Delta) {
					return Result{}, ErrValue
				}
				acc.Value += op.Delta
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if err := l.checkValue(acc.Value); err != nil {
				return Result{}, err
			}
			revision++
			acc.Revision = revision
			if _, ok := cand[op.Name]; !ok {
				order = append(order, op.Name)
			}
			cand[op.Name] = pending{acc: acc, seen: true}
		case Set:
			if err := l.checkValue(op.Value); err != nil {
				return Result{}, err
			}
			revision++
			acc := Account{Name: op.Name, Value: op.Value, Revision: revision}
			if _, ok := cand[op.Name]; !ok {
				order = append(order, op.Name)
			}
			cand[op.Name] = pending{acc: acc, seen: true}
		case Delete:
			if _, ok := get(op.Name); !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := cand[op.Name]; !ok {
				order = append(order, op.Name)
			}
			cand[op.Name] = pending{seen: false}
		}
	}

	// 最终账户容量仅在批次末检查。
	final := len(l.accounts)
	for name, p := range cand {
		_, exists := l.accounts[name]
		switch {
		case p.seen && !exists:
			final++
		case !p.seen && exists:
			final--
		}
	}
	if final > l.maxAcct {
		return Result{}, ErrCapacity
	}

	changed := make([]Account, 0, len(order))
	for _, name := range order {
		p := cand[name]
		if p.seen {
			l.accounts[name] = p.acc
			changed = append(changed, p.acc)
		} else {
			delete(l.accounts, name)
		}
	}
	l.revision = revision
	l.generation++
	return Result{Generation: l.generation, Revision: revision, Changed: changed}, nil
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
	out := make([]Account, n)
	copy(out, all[:n])
	return out, nil
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
