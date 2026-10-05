package prefixacl

import (
	"errors"
	"net/netip"
	"slices"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Action uint8

const (
	Allow Action = iota + 1
	Deny
)

type Kind uint8

const (
	Upsert Kind = iota + 1
	Delete
)

type Options struct {
	MaxRules int
	Default  Action
}
type Op struct {
	Kind   Kind
	Prefix netip.Prefix
	Action Action
}
type Batch struct{ Ops []Op }
type Rule struct {
	Prefix   netip.Prefix
	Action   Action
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Rule
}
type Snapshot struct {
	Generation, NextRevision uint64
	Rules                    []Rule
}
type Table struct {
	mu           sync.RWMutex
	maxRules     int
	defaultAct   Action
	rules        map[netip.Prefix]Rule
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Table, error) {
	if o.MaxRules <= 0 || (o.Default != Allow && o.Default != Deny) {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxRules:     o.MaxRules,
		defaultAct:   o.Default,
		rules:        make(map[netip.Prefix]Rule),
		nextRevision: 1,
	}, nil
}

func validateOp(op Op) error {
	if !op.Prefix.IsValid() || op.Prefix != op.Prefix.Masked() {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Upsert:
		if op.Action != Allow && op.Action != Deny {
			return ErrInvalidInput
		}
	case Delete:
		if op.Action != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func sortRules(rules []Rule) {
	slices.SortFunc(rules, func(a, b Rule) int {
		a4, b4 := a.Prefix.Addr().Is4(), b.Prefix.Addr().Is4()
		if a4 != b4 {
			if a4 {
				return -1
			}
			return 1
		}
		if c := a.Prefix.Masked().Addr().Compare(b.Prefix.Masked().Addr()); c != 0 {
			return c
		}
		if c := a.Prefix.Bits() - b.Prefix.Bits(); c != 0 {
			return c
		}
		return int(a.Action) - int(b.Action)
	})
}

func (t *Table) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		t.mu.RLock()
		r := Result{Generation: t.generation, Revision: t.nextRevision - 1}
		t.mu.RUnlock()
		return r, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	cand := make(map[netip.Prefix]Rule, len(t.rules)+len(b.Ops))
	for p, r := range t.rules {
		cand[p] = r
	}
	rev := t.nextRevision
	touched := map[netip.Prefix]struct{}{}
	for _, op := range b.Ops {
		switch op.Kind {
		case Upsert:
			cand[op.Prefix] = Rule{Prefix: op.Prefix, Action: op.Action, Revision: rev}
			rev++
			touched[op.Prefix] = struct{}{}
		case Delete:
			if _, ok := cand[op.Prefix]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Prefix)
		}
	}
	if len(cand) > t.maxRules {
		return Result{}, ErrCapacity
	}

	changed := make([]Rule, 0, len(touched))
	for p := range touched {
		if r, ok := cand[p]; ok {
			changed = append(changed, r)
		}
	}
	sortRules(changed)

	t.rules = cand
	t.generation++
	t.nextRevision = rev
	return Result{Generation: t.generation, Revision: rev - 1, Changed: changed}, nil
}

func (t *Table) Lookup(addr netip.Addr) (Action, netip.Prefix, bool, error) {
	if !addr.IsValid() {
		return 0, netip.Prefix{}, false, ErrInvalidInput
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	best := -1
	var action Action
	var prefix netip.Prefix
	for p, r := range t.rules {
		if p.Addr().Is4() != addr.Is4() {
			continue
		}
		if p.Bits() > best && p.Contains(addr) {
			best = p.Bits()
			action = r.Action
			prefix = p
		}
	}
	if best < 0 {
		return t.defaultAct, netip.Prefix{}, false, nil
	}
	return action, prefix, true, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	rules := make([]Rule, 0, len(t.rules))
	for _, r := range t.rules {
		rules = append(rules, r)
	}
	sortRules(rules)
	return Snapshot{Generation: t.generation, NextRevision: t.nextRevision, Rules: rules}
}
