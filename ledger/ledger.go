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
	maxAccts   int
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
		maxAccts: o.MaxAccounts,
		maxName:  o.MaxNameBytes,
		accounts: make(map[string]Account),
	}, nil
}

func validNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '/', c == '-':
		return true
	}
	return false
}

func (l *Ledger) validName(s string) bool {
	if s == "" || len(s) > l.maxName {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !validNameByte(s[i]) {
			return false
		}
	}
	return true
}

func (l *Ledger) validateOp(o Op) error {
	if !l.validName(o.Account) {
		return ErrInvalidInput
	}
	switch o.Kind {
	case Open:
		if o.Amount < 0 || o.Other != "" {
			return ErrInvalidInput
		}
	case Credit, Debit:
		if o.Amount <= 0 || o.Other != "" {
			return ErrInvalidInput
		}
	case Transfer:
		if o.Amount <= 0 || !l.validName(o.Other) || o.Other == o.Account {
			return ErrInvalidInput
		}
	case Close:
		if o.Amount != 0 || o.Other != "" {
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

	for _, o := range b.Ops {
		if err := l.validateOp(o); err != nil {
			return Result{}, err
		}
	}

	cand := make(map[string]Account, len(l.accounts)+len(b.Ops))
	for k, v := range l.accounts {
		cand[k] = v
	}
	rev := l.revision
	touched := make(map[string]struct{})

	for _, o := range b.Ops {
		switch o.Kind {
		case Open:
			if _, ok := cand[o.Account]; ok {
				return Result{}, ErrConflict
			}
			rev++
			cand[o.Account] = Account{Name: o.Account, Balance: o.Amount, Revision: rev}
			touched[o.Account] = struct{}{}
		case Credit:
			a, ok := cand[o.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if a.Balance > 0 && o.Amount > (1<<63-1)-a.Balance {
				return Result{}, ErrOverflow
			}
			rev++
			a.Balance += o.Amount
			a.Revision = rev
			cand[o.Account] = a
			touched[o.Account] = struct{}{}
		case Debit:
			a, ok := cand[o.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if o.Amount > a.Balance {
				return Result{}, ErrFunds
			}
			rev++
			a.Balance -= o.Amount
			a.Revision = rev
			cand[o.Account] = a
			touched[o.Account] = struct{}{}
		case Transfer:
			src, ok := cand[o.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			dst, ok := cand[o.Other]
			if !ok {
				return Result{}, ErrNotFound
			}
			if o.Amount > src.Balance {
				return Result{}, ErrFunds
			}
			if dst.Balance > 0 && o.Amount > (1<<63-1)-dst.Balance {
				return Result{}, ErrOverflow
			}
			rev++
			src.Balance -= o.Amount
			src.Revision = rev
			dst.Balance += o.Amount
			dst.Revision = rev
			cand[o.Account] = src
			cand[o.Other] = dst
			touched[o.Account] = struct{}{}
			touched[o.Other] = struct{}{}
		case Close:
			a, ok := cand[o.Account]
			if !ok {
				return Result{}, ErrNotFound
			}
			if a.Balance != 0 {
				return Result{}, ErrBalance
			}
			rev++
			delete(cand, o.Account)
			delete(touched, o.Account)
		}
	}

	if len(cand) > l.maxAccts {
		return Result{}, ErrCapacity
	}

	l.accounts = cand
	l.revision = rev
	if len(b.Ops) > 0 {
		l.generation++
	}

	changed := make([]Account, 0, len(touched))
	for name := range touched {
		if a, ok := cand[name]; ok {
			changed = append(changed, a)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })

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
	accts := make([]Account, 0, len(l.accounts))
	for _, a := range l.accounts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool { return accts[i].Name < accts[j].Name })
	return Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Accounts:     accts,
	}
}
