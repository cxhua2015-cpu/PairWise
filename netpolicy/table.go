package netpolicy

import (
	"fmt"
	"net/netip"
	"sort"
	"sync"
)

const (
	maxRulesLimit      = 10000
	maxValueBytesLimit = 64 << 20
	maxIDLen           = 64
	maxValueLen        = 1 << 20
	minPriority        = -1000
	maxPriority        = 1000
	anyPortWidth       = 1 << 16
)

type storedRule struct {
	id       string
	prefix   netip.Prefix
	canon    string
	protocol Protocol
	portLo   uint16
	portHi   uint16
	priority int
	action   Action
	value    []byte
}

// Table is a concurrency-safe in-memory network policy table.
type Table struct {
	mu        sync.RWMutex
	maxRules  int
	maxBytes  int
	rules     map[string]*storedRule
	usedBytes int
	gen       uint64
}

// New creates a Table enforcing the given capacity budgets.
func New(o Options) (*Table, error) {
	if o.MaxRules < 1 || o.MaxRules > maxRulesLimit ||
		o.MaxValueBytes < 1 || o.MaxValueBytes > maxValueBytesLimit {
		return nil, fmt.Errorf("netpolicy: options %+v: %w", o, ErrInvalidOptions)
	}
	return &Table{
		maxRules: o.MaxRules,
		maxBytes: o.MaxValueBytes,
		rules:    make(map[string]*storedRule),
	}, nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > maxIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func canonicalPrefix(s string) (netip.Prefix, string, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: prefix %q: %w", s, ErrInvalidPrefix)
	}
	p = unmapPrefix(p).Masked()
	return p, p.String(), nil
}

// unmapPrefix converts an IPv4-mapped IPv6 prefix to its IPv4 form.
func unmapPrefix(p netip.Prefix) netip.Prefix {
	addr := p.Addr()
	if !addr.Is4In6() {
		return p
	}
	if p.Bits() < 96 {
		return p
	}
	return netip.PrefixFrom(addr.Unmap(), p.Bits()-96)
}

func validateRule(r *Rule) (netip.Prefix, string, error) {
	if !validID(r.ID) {
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: id %q: %w", r.ID, ErrInvalidID)
	}
	p, canon, err := canonicalPrefix(r.Prefix)
	if err != nil {
		return netip.Prefix{}, "", err
	}
	switch r.Protocol {
	case ProtocolAny:
		if r.PortStart != 0 || r.PortEnd != 0 {
			return netip.Prefix{}, "", fmt.Errorf("netpolicy: any protocol requires ports 0..0: %w", ErrInvalidPort)
		}
	case ProtocolTCP, ProtocolUDP:
		if r.PortStart > r.PortEnd {
			return netip.Prefix{}, "", fmt.Errorf("netpolicy: port range %d..%d: %w", r.PortStart, r.PortEnd, ErrInvalidPort)
		}
	default:
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: protocol %d: %w", r.Protocol, ErrInvalidProtocol)
	}
	if r.Priority < minPriority || r.Priority > maxPriority {
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: priority %d: %w", r.Priority, ErrInvalidPriority)
	}
	if r.Action != ActionAllow && r.Action != ActionDeny {
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: action %d: %w", r.Action, ErrInvalidAction)
	}
	if len(r.Value) > maxValueLen {
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: value of %d bytes: %w", len(r.Value), ErrValueTooLarge)
	}
	return p, canon, nil
}

// validateChange performs the structural validation phase for one change.
func validateChange(c *Change) (netip.Prefix, string, error) {
	switch c.Type {
	case ChangeAdd:
		if c.ID != "" {
			return netip.Prefix{}, "", fmt.Errorf("netpolicy: add change must not set ID: %w", ErrInvalidChange)
		}
		return validateRule(&c.Rule)
	case ChangeDelete:
		if c.Rule.ID != "" {
			return netip.Prefix{}, "", fmt.Errorf("netpolicy: delete change must not set Rule.ID: %w", ErrInvalidChange)
		}
		if !validID(c.ID) {
			return netip.Prefix{}, "", fmt.Errorf("netpolicy: id %q: %w", c.ID, ErrInvalidID)
		}
		return netip.Prefix{}, "", nil
	default:
		return netip.Prefix{}, "", fmt.Errorf("netpolicy: change type %d: %w", c.Type, ErrInvalidChange)
	}
}

// Apply applies a single change; it is equivalent to a one-element ApplyBatch.
func (t *Table) Apply(c Change) (uint64, error) {
	return t.ApplyBatch([]Change{c})
}

// ApplyBatch validates all changes, then applies them in order on an
// isolated candidate state. On any failure the table is left untouched.
func (t *Table) ApplyBatch(changes []Change) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(changes) == 0 {
		return t.gen, nil
	}

	// Phase 1: structural validation of every change, in input order.
	type parsed struct {
		change Change
		prefix netip.Prefix
		canon  string
	}
	prepared := make([]parsed, len(changes))
	for i := range changes {
		p, canon, err := validateChange(&changes[i])
		if err != nil {
			return t.gen, err
		}
		prepared[i] = parsed{change: changes[i], prefix: p, canon: canon}
	}

	// Phase 2: semantic operations on an isolated candidate, in input order.
	candidate := make(map[string]*storedRule, len(t.rules)+len(changes))
	for id, r := range t.rules {
		candidate[id] = r
	}
	used := t.usedBytes
	for _, pp := range prepared {
		c := pp.change
		switch c.Type {
		case ChangeAdd:
			if _, ok := candidate[c.Rule.ID]; ok {
				return t.gen, fmt.Errorf("netpolicy: id %q: %w", c.Rule.ID, ErrDuplicateID)
			}
			var value []byte
			if len(c.Rule.Value) > 0 {
				value = make([]byte, len(c.Rule.Value))
				copy(value, c.Rule.Value)
			}
			candidate[c.Rule.ID] = &storedRule{
				id:       c.Rule.ID,
				prefix:   pp.prefix,
				canon:    pp.canon,
				protocol: c.Rule.Protocol,
				portLo:   c.Rule.PortStart,
				portHi:   c.Rule.PortEnd,
				priority: c.Rule.Priority,
				action:   c.Rule.Action,
				value:    value,
			}
			used += len(value)
		case ChangeDelete:
			old, ok := candidate[c.ID]
			if !ok {
				return t.gen, fmt.Errorf("netpolicy: id %q: %w", c.ID, ErrNotFound)
			}
			delete(candidate, c.ID)
			used -= len(old.value)
		}
	}

	// Capacity is only checked against the final state.
	if len(candidate) > t.maxRules || used > t.maxBytes {
		return t.gen, fmt.Errorf("netpolicy: rules=%d bytes=%d: %w", len(candidate), used, ErrCapacity)
	}

	t.rules = candidate
	t.usedBytes = used
	t.gen++
	return t.gen, nil
}

func portWidth(r *storedRule) int {
	if r.protocol == ProtocolAny {
		return anyPortWidth
	}
	return int(r.portHi) - int(r.portLo)
}

// better reports whether candidate a outranks candidate b under the stable
// precedence keys: prefix bits, protocol specificity, port width, priority,
// action, then ID.
func better(a, b *storedRule) bool {
	if a.prefix.Bits() != b.prefix.Bits() {
		return a.prefix.Bits() > b.prefix.Bits()
	}
	if a.protocol != b.protocol {
		return a.protocol != ProtocolAny
	}
	aw, bw := portWidth(a), portWidth(b)
	if aw != bw {
		return aw < bw
	}
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	if a.action != b.action {
		return a.action == ActionDeny
	}
	return a.id < b.id
}

// Match resolves the best rule for (address, protocol, port).
func (t *Table) Match(address string, protocol Protocol, port uint16) (Decision, error) {
	addr, err := netip.ParseAddr(address)
	if err != nil || addr.Zone() != "" {
		return Decision{}, fmt.Errorf("netpolicy: address %q: %w", address, ErrInvalidAddress)
	}
	addr = addr.Unmap()
	if protocol != ProtocolTCP && protocol != ProtocolUDP {
		return Decision{}, fmt.Errorf("netpolicy: protocol %d: %w", protocol, ErrInvalidProtocol)
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	var best *storedRule
	for _, r := range t.rules {
		if !r.prefix.Contains(addr) {
			continue
		}
		if r.protocol != ProtocolAny {
			if r.protocol != protocol {
				continue
			}
			if port < r.portLo || port > r.portHi {
				continue
			}
		}
		if best == nil || better(r, best) {
			best = r
		}
	}
	if best == nil {
		return Decision{Found: false, Generation: t.gen}, nil
	}
	var value []byte
	if len(best.value) > 0 {
		value = make([]byte, len(best.value))
		copy(value, best.value)
	}
	return Decision{
		Found:      true,
		Action:     best.action,
		RuleID:     best.id,
		Prefix:     best.canon,
		Protocol:   best.protocol,
		Value:      value,
		Generation: t.gen,
	}, nil
}

// Snapshot returns a consistently ordered, deep-copied view of the table.
func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	views := make([]RuleView, 0, len(t.rules))
	for _, r := range t.rules {
		var value []byte
		if len(r.value) > 0 {
			value = make([]byte, len(r.value))
			copy(value, r.value)
		}
		views = append(views, RuleView{
			ID:        r.id,
			Prefix:    r.canon,
			Protocol:  r.protocol,
			PortStart: r.portLo,
			PortEnd:   r.portHi,
			Priority:  r.priority,
			Action:    r.action,
			Value:     value,
		})
	}
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		pa, _ := netip.ParsePrefix(a.Prefix)
		pb, _ := netip.ParsePrefix(b.Prefix)
		if c := pa.Addr().Compare(pb.Addr()); c != 0 {
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
	return Snapshot{
		Generation:     t.gen,
		UsedValueBytes: t.usedBytes,
		Rules:          views,
	}
}
