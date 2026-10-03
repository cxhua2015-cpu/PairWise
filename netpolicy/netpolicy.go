package netpolicy

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
)

var (
	ErrNotImplemented  = errors.New("netpolicy: not implemented")
	ErrInvalidOptions  = errors.New("netpolicy: invalid options")
	ErrInvalidChange   = errors.New("netpolicy: invalid change")
	ErrInvalidID       = errors.New("netpolicy: invalid id")
	ErrInvalidPrefix   = errors.New("netpolicy: invalid prefix")
	ErrInvalidProtocol = errors.New("netpolicy: invalid protocol")
	ErrInvalidPort     = errors.New("netpolicy: invalid port range")
	ErrInvalidPriority = errors.New("netpolicy: invalid priority")
	ErrInvalidAction   = errors.New("netpolicy: invalid action")
	ErrValueTooLarge   = errors.New("netpolicy: value too large")
	ErrDuplicateID     = errors.New("netpolicy: duplicate id")
	ErrNotFound        = errors.New("netpolicy: rule not found")
	ErrCapacity        = errors.New("netpolicy: capacity exceeded")
	ErrInvalidAddress  = errors.New("netpolicy: invalid address")
)

type Protocol uint8

const (
	ProtocolAny Protocol = iota
	ProtocolTCP
	ProtocolUDP
)

type Action uint8

const (
	ActionAllow Action = iota
	ActionDeny
)

type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeDelete
)

type Options struct{ MaxRules, MaxValueBytes int }
type Rule struct {
	ID, Prefix         string
	Protocol           Protocol
	PortStart, PortEnd uint16
	Priority           int
	Action             Action
	Value              []byte
}
type Change struct {
	Type ChangeType
	Rule Rule
	ID   string
}
type Decision struct {
	Found          bool
	Action         Action
	RuleID, Prefix string
	Protocol       Protocol
	Value          []byte
	Generation     uint64
}
type RuleView struct {
	ID, Prefix         string
	Protocol           Protocol
	PortStart, PortEnd uint16
	Priority           int
	Action             Action
	Value              []byte
}
type Snapshot struct {
	Generation     uint64
	UsedValueBytes int
	Rules          []RuleView
}

const (
	maxRulesLimit      = 10000
	maxValueBytesLimit = 64 << 20
	maxIDLen           = 64
	maxValueLen        = 1 << 20
	minPriority        = -1000
	maxPriority        = 1000
)

type entry struct {
	rule   Rule
	prefix netip.Prefix
}

type Table struct {
	mu             sync.RWMutex
	maxRules       int
	maxValueBytes  int
	rules          map[string]*entry
	usedValueBytes int
	generation     uint64
}

func New(o Options) (*Table, error) {
	if o.MaxRules < 1 || o.MaxRules > maxRulesLimit ||
		o.MaxValueBytes < 1 || o.MaxValueBytes > maxValueBytesLimit {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxRules:      o.MaxRules,
		maxValueBytes: o.MaxValueBytes,
		rules:         make(map[string]*entry),
	}, nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > maxIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func canonicalPrefix(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, ErrInvalidPrefix
	}
	addr := p.Addr()
	bits := p.Bits()
	if addr.Is4In6() {
		addr = addr.Unmap()
		bits -= 96
		if bits < 0 {
			bits = 0
		}
	}
	return netip.PrefixFrom(addr, bits).Masked(), nil
}

func validateRule(r *Rule) (netip.Prefix, error) {
	if !validID(r.ID) {
		return netip.Prefix{}, ErrInvalidID
	}
	p, err := canonicalPrefix(r.Prefix)
	if err != nil {
		return netip.Prefix{}, err
	}
	if r.Protocol != ProtocolAny && r.Protocol != ProtocolTCP && r.Protocol != ProtocolUDP {
		return netip.Prefix{}, ErrInvalidProtocol
	}
	if r.Protocol == ProtocolAny {
		if r.PortStart != 0 || r.PortEnd != 0 {
			return netip.Prefix{}, ErrInvalidPort
		}
	} else if r.PortStart > r.PortEnd {
		return netip.Prefix{}, ErrInvalidPort
	}
	if r.Priority < minPriority || r.Priority > maxPriority {
		return netip.Prefix{}, ErrInvalidPriority
	}
	if r.Action != ActionAllow && r.Action != ActionDeny {
		return netip.Prefix{}, ErrInvalidAction
	}
	if len(r.Value) > maxValueLen {
		return netip.Prefix{}, ErrValueTooLarge
	}
	return p, nil
}

func validateChange(c *Change) error {
	switch c.Type {
	case ChangeAdd:
		if c.ID != "" {
			return ErrInvalidChange
		}
		_, err := validateRule(&c.Rule)
		return err
	case ChangeDelete:
		if c.Rule.ID != "" {
			return ErrInvalidChange
		}
		if !validID(c.ID) {
			return ErrInvalidID
		}
		return nil
	default:
		return ErrInvalidChange
	}
}

func (t *Table) Apply(c Change) (uint64, error) {
	return t.ApplyBatch([]Change{c})
}

func (t *Table) ApplyBatch(changes []Change) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(changes) == 0 {
		return t.generation, nil
	}
	for i := range changes {
		if err := validateChange(&changes[i]); err != nil {
			return t.generation, fmt.Errorf("change %d: %w", i, err)
		}
	}
	rules := make(map[string]*entry, len(t.rules)+len(changes))
	for id, e := range t.rules {
		rules[id] = e
	}
	used := t.usedValueBytes
	for i := range changes {
		c := &changes[i]
		if c.Type == ChangeAdd {
			if _, ok := rules[c.Rule.ID]; ok {
				return t.generation, fmt.Errorf("change %d: %w", i, ErrDuplicateID)
			}
			p, _ := canonicalPrefix(c.Rule.Prefix)
			r := c.Rule
			r.Prefix = p.String()
			r.Value = bytes.Clone(r.Value)
			rules[r.ID] = &entry{rule: r, prefix: p}
			used += len(r.Value)
		} else {
			e, ok := rules[c.ID]
			if !ok {
				return t.generation, fmt.Errorf("change %d: %w", i, ErrNotFound)
			}
			used -= len(e.rule.Value)
			delete(rules, c.ID)
		}
	}
	if len(rules) > t.maxRules || used > t.maxValueBytes {
		return t.generation, ErrCapacity
	}
	t.rules = rules
	t.usedValueBytes = used
	t.generation++
	return t.generation, nil
}

func portWidth(e *entry) int {
	if e.rule.Protocol == ProtocolAny {
		return 65536
	}
	return int(e.rule.PortEnd) - int(e.rule.PortStart) + 1
}

func better(a, b *entry) bool {
	pa, pb := a.prefix.Bits(), b.prefix.Bits()
	if pa != pb {
		return pa > pb
	}
	sa, sb := a.rule.Protocol != ProtocolAny, b.rule.Protocol != ProtocolAny
	if sa != sb {
		return sa
	}
	wa, wb := portWidth(a), portWidth(b)
	if wa != wb {
		return wa < wb
	}
	if a.rule.Priority != b.rule.Priority {
		return a.rule.Priority > b.rule.Priority
	}
	if a.rule.Action != b.rule.Action {
		return a.rule.Action == ActionDeny
	}
	return a.rule.ID < b.rule.ID
}

func (t *Table) Match(addr string, proto Protocol, port uint16) (Decision, error) {
	a, err := netip.ParseAddr(addr)
	if err != nil || a.Zone() != "" {
		return Decision{}, ErrInvalidAddress
	}
	a = a.Unmap()
	if proto != ProtocolTCP && proto != ProtocolUDP {
		return Decision{}, ErrInvalidProtocol
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	d := Decision{Generation: t.generation}
	var best *entry
	for _, e := range t.rules {
		if !e.prefix.Contains(a) {
			continue
		}
		if e.rule.Protocol != ProtocolAny {
			if e.rule.Protocol != proto {
				continue
			}
			if port < e.rule.PortStart || port > e.rule.PortEnd {
				continue
			}
		}
		if best == nil || better(e, best) {
			best = e
		}
	}
	if best == nil {
		return d, nil
	}
	d.Found = true
	d.Action = best.rule.Action
	d.RuleID = best.rule.ID
	d.Prefix = best.rule.Prefix
	d.Protocol = best.rule.Protocol
	d.Value = bytes.Clone(best.rule.Value)
	return d, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := Snapshot{
		Generation:     t.generation,
		UsedValueBytes: t.usedValueBytes,
		Rules:          make([]RuleView, 0, len(t.rules)),
	}
	for _, e := range t.rules {
		s.Rules = append(s.Rules, RuleView{
			ID:        e.rule.ID,
			Prefix:    e.rule.Prefix,
			Protocol:  e.rule.Protocol,
			PortStart: e.rule.PortStart,
			PortEnd:   e.rule.PortEnd,
			Priority:  e.rule.Priority,
			Action:    e.rule.Action,
			Value:     bytes.Clone(e.rule.Value),
		})
	}
	sort.Slice(s.Rules, func(i, j int) bool {
		a, b := s.Rules[i], s.Rules[j]
		pa := t.rules[a.ID].prefix
		pb := t.rules[b.ID].prefix
		aa, ab := pa.Addr(), pb.Addr()
		if aa.Is4() != ab.Is4() {
			return aa.Is4()
		}
		if c := aa.Compare(ab); c != 0 {
			return c < 0
		}
		if pa.Bits() != pb.Bits() {
			return pa.Bits() > pb.Bits()
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.PortStart != b.PortStart {
			return a.PortStart < b.PortStart
		}
		if a.PortEnd != b.PortEnd {
			return a.PortEnd < b.PortEnd
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		return a.ID < b.ID
	})
	return s
}
