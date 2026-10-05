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
	mu         sync.RWMutex
	maxRules   int
	def        Action
	rules      map[netip.Prefix]Rule
	generation uint64
	revision   uint64
}

func New(o Options) (*Table, error) {
	if o.MaxRules <= 0 || (o.Default != Allow && o.Default != Deny) {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxRules: o.MaxRules,
		def:      o.Default,
		rules:    make(map[netip.Prefix]Rule),
	}, nil
}

func validateOp(op Op) error {
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
	if !op.Prefix.IsValid() || op.Prefix != op.Prefix.Masked() {
		return ErrInvalidInput
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if err := validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return Result{}, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Execute on an isolated candidate in input order.
	cand := make(map[netip.Prefix]Rule, len(t.rules)+len(b.Ops))
	for p, r := range t.rules {
		cand[p] = r
	}
	revision := t.revision
	touched := map[netip.Prefix]bool{}
	for _, op := range b.Ops {
		switch op.Kind {
		case Upsert:
			revision++
			cand[op.Prefix] = Rule{Prefix: op.Prefix, Action: op.Action, Revision: revision}
			touched[op.Prefix] = true
		case Delete:
			if _, ok := cand[op.Prefix]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Prefix)
		}
	}
	// Capacity is checked only against the final state.
	if len(cand) > t.maxRules {
		return Result{}, ErrCapacity
	}

	t.rules = cand
	t.revision = revision
	t.generation++

	res := Result{Generation: t.generation, Revision: revision}
	for _, r := range sortRules(cand) {
		if touched[r.Prefix] {
			res.Changed = append(res.Changed, r)
		}
	}
	return res, nil
}

func (t *Table) Lookup(addr netip.Addr) (Action, netip.Prefix, bool, error) {
	if !addr.IsValid() {
		return 0, netip.Prefix{}, false, ErrInvalidInput
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	best := -1
	var br Rule
	for p, r := range t.rules {
		if p.Addr().Is4() != addr.Is4() || p.Bits() <= best {
			continue
		}
		if p.Contains(addr) {
			best = p.Bits()
			br = r
		}
	}
	if best < 0 {
		return t.def, netip.Prefix{}, false, nil
	}
	return br.Action, br.Prefix, true, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.revision + 1,
		Rules:        sortRules(t.rules),
	}
}

func sortRules(m map[netip.Prefix]Rule) []Rule {
	rules := make([]Rule, 0, len(m))
	for _, r := range m {
		rules = append(rules, r)
	}
	slices.SortFunc(rules, func(a, b Rule) int {
		if c := a.Prefix.Addr().Compare(b.Prefix.Addr()); c != 0 {
			return c
		}
		if c := a.Prefix.Bits() - b.Prefix.Bits(); c != 0 {
			return c
		}
		return int(a.Action) - int(b.Action)
	})
	return rules
}
