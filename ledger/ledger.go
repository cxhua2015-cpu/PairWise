package ledger

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrFunds          = errors.New("insufficient funds")
	ErrBalance        = errors.New("nonzero balance")
	ErrOverflow       = errors.New("overflow")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxAccounts, MaxNameBytes int }

type OpKind uint8

const (
	Open OpKind = iota + 1
	Credit
	Debit
	Transfer
	Close
)

type Op struct {
	Kind           OpKind
	Account, Other string
	Amount         int64
}

type Batch struct{ Ops []Op }

type Account struct {
	Name     string
	Balance  int64
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
	accounts   map[string]Account
	generation uint64
	revision   uint64
}

func New(o Options) (*Ledger, error) {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{
		maxAcct:  o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		accounts: make(map[string]Account),
	}, nil
}

func (l *Ledger) validName(s string) bool {
	if s == "" || len(s) > l.maxName {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func (l *Ledger) validate(op Op) error {
	switch op.Kind {
	case Open:
		if !l.validName(op.Account) || op.Amount < 0 {
			return ErrInvalidInput
		}
	case Credit, Debit:
		if !l.validName(op.Account) || op.Amount <= 0 || op.Other != "" {
			return ErrInvalidInput
		}
	case Transfer:
		if !l.validName(op.Account) || !l.validName(op.Other) ||
			op.Account == op.Other || op.Amount <= 0 {
			return ErrInvalidInput
		}
	case Close:
		if !l.validName(op.Account) || op.Amount != 0 || op.Other != "" {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (l *Ledger) Apply(b Batch) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, op := range b.Ops {
		if err := l.validate(op); err != nil {
			return Result{}, err
		}
	}

	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Open:
			if _, ok := cand[op.Account]; ok {
				return Result{}, ErrConflict
			}
			rev++
			cand[op.Account] = Account{Name: op.Account, Balance: op.Amount, Revision: rev}
			touched[op.Account] = struct{}{}
		case Credit:
			a, ok := cand[op.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if a.Balance > 0 && op.Amount > (1<<63-1)-a.Balance {
				return Result{}, ErrOverflow
			}
			rev++
			a.Balance += op.Amount
			a.Revision = rev
			cand[op.Account] = a
			touched[op.Account] = struct{}{}
		case Debit:
			a, ok := cand[op.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if op.Amount > a.Balance {
				return Result{}, ErrFunds
			}
			rev++
			a.Balance -= op.Amount
			a.Revision = rev
			cand[op.Account] = a
			touched[op.Account] = struct{}{}
		case Transfer:
			src, ok := cand[op.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			dst, ok := cand[op.Other]
			if !ok {
				return Result{}, ErrNotFound
			}
			if op.Amount > src.Balance {
				return Result{}, ErrFunds
			}
			if dst.Balance > 0 && op.Amount > (1<<63-1)-dst.Balance {
				return Result{}, ErrOverflow
			}
			rev++
			src.Balance -= op.Amount
			src.Revision = rev
			dst.Balance += op.Amount
			dst.Revision = rev
			cand[op.Account] = src
			cand[op.Other] = dst
			touched[op.Account] = struct{}{}
			touched[op.Other] = struct{}{}
		case Close:
			a, ok := cand[op.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if a.Balance != 0 {
				return Result{}, ErrBalance
			}
			rev++
			delete(cand, op.Account)
			delete(touched, op.Account)
		}
	}

	if len(cand) > l.maxAcct {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.revision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}

	names := make([]string, 0, len(touched))
	for n := range touched {
		names = append(names, n)
	}
	sort.Strings(names)
	changed := make([]Account, 0, len(names))
	for _, n := range names {
		changed = append(changed, cand[n])
	}
	return Result{Generation: l.generation, Revision: l.revision, Changed: changed}, nil
}

func (l *Ledger) Get(name string) (Account, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.validName(name) {
		return Account{}, false, ErrInvalidInput
	}
	a, ok := l.accounts[name]
	return a, ok, nil
}

func (l *Ledger) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, 0, len(l.accounts))
	for n := range l.accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	accts := make([]Account, 0, len(names))
	for _, n := range names {
		accts = append(accts, l.accounts[n])
	}
	return Snapshot{Generation: l.generation, NextRevision: l.revision + 1, Accounts: accts}
}
