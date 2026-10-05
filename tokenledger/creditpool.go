package tokenledger

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
	mu       sync.Mutex
	opts     Options
	accounts map[string]Account
	gen      uint64
	nextRev  uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: o, accounts: make(map[string]Account), nextRev: 1}, nil
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

func absOK(v, maxAbs int64) bool {
	if v == math.MinInt64 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= maxAbs
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	// 结构校验：在读取任何状态前完成全部校验。
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

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: l.gen, Revision: l.nextRev - 1}, nil
	}

	// 候选事务：在副本上按输入顺序应用，失败则整体丢弃。
	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	nextRev := l.nextRev
	touched := make(map[string]bool, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			acc, ok := cand[op.Name]
			if ok {
				// 算术前检测 int64 溢出。
				d := op.Delta
				if (d > 0 && acc.Value > math.MaxInt64-d) || (d < 0 && acc.Value < math.MinInt64-d) {
					return Result{}, ErrValue
				}
				acc.Value += d
			} else {
				acc = Account{Name: op.Name, Value: op.Delta}
			}
			if !absOK(acc.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			acc.Revision = nextRev
			nextRev++
			cand[op.Name] = acc
			touched[op.Name] = true
		case Set:
			if !absOK(op.Value, l.opts.MaxAbsValue) {
				return Result{}, ErrValue
			}
			cand[op.Name] = Account{Name: op.Name, Value: op.Value, Revision: nextRev}
			nextRev++
			touched[op.Name] = true
		case Delete:
			if _, ok := cand[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Name)
			touched[op.Name] = true
		}
	}

	// 最终账户容量仅在批次末检查。
	if len(cand) > l.opts.MaxAccounts {
		return Result{}, ErrCapacity
	}

	// 提交。
	l.accounts = cand
	l.gen++
	l.nextRev = nextRev

	changed := make([]Account, 0, len(touched))
	for name := range touched {
		if acc, ok := cand[name]; ok {
			changed = append(changed, acc)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return Result{Generation: l.gen, Revision: nextRev - 1, Changed: changed}, nil
}

func (l *Ledger) Top(n int) ([]Account, error) {
	if n < 0 {
		return nil, ErrInvalidInput
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	all := make([]Account, 0, len(l.accounts))
	for _, acc := range l.accounts {
		all = append(all, acc)
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
	for _, acc := range l.accounts {
		accs = append(accs, acc)
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].Name < accs[j].Name })
	return Snapshot{Generation: l.gen, NextRevision: l.nextRev, Accounts: accs}
}
