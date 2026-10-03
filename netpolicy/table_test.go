package netpolicy

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func mustAdd(t *testing.T, tab *Table, r Rule) {
	t.Helper()
	if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: r}); err != nil {
		t.Fatalf("add %s: %v", r.ID, err)
	}
}

func TestFieldValidationBoundaries(t *testing.T) {
	tab, err := New(Options{MaxRules: 100, MaxValueBytes: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	base := Rule{ID: "ok", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 2, Action: ActionAllow}
	cases := []struct {
		name string
		mut  func(*Rule)
		want error
	}{
		{"empty id", func(r *Rule) { r.ID = "" }, ErrInvalidID},
		{"long id", func(r *Rule) { r.ID = strings.Repeat("a", 65) }, ErrInvalidID},
		{"bad id char", func(r *Rule) { r.ID = "a b" }, ErrInvalidID},
		{"bad prefix", func(r *Rule) { r.Prefix = "10.0.0.0/33" }, ErrInvalidPrefix},
		{"zone prefix", func(r *Rule) { r.Prefix = "fe80::1%eth0/64" }, ErrInvalidPrefix},
		{"bad protocol", func(r *Rule) { r.Protocol = Protocol(9) }, ErrInvalidProtocol},
		{"any with ports", func(r *Rule) { r.Protocol = ProtocolAny; r.PortEnd = 80 }, ErrInvalidPort},
		{"reversed ports", func(r *Rule) { r.PortStart, r.PortEnd = 90, 80 }, ErrInvalidPort},
		{"priority low", func(r *Rule) { r.Priority = -1001 }, ErrInvalidPriority},
		{"priority high", func(r *Rule) { r.Priority = 1001 }, ErrInvalidPriority},
		{"bad action", func(r *Rule) { r.Action = Action(7) }, ErrInvalidAction},
		{"value too large", func(r *Rule) { r.Value = make([]byte, (1<<20)+1) }, ErrValueTooLarge},
	}
	for _, tc := range cases {
		r := base
		tc.mut(&r)
		if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: r}); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, err, tc.want)
		}
	}
	// Boundary values that must be accepted.
	ok := []Rule{
		{ID: strings.Repeat("a-Z.0_", 11)[:64], Prefix: "0.0.0.0/0", Protocol: ProtocolAny, Action: ActionAllow},
		{ID: "edge-pri", Prefix: "::/0", Protocol: ProtocolAny, Priority: 1000, Action: ActionDeny},
		{ID: "edge-pri2", Prefix: "11.0.0.0/8", Protocol: ProtocolUDP, PortStart: 0, PortEnd: 0, Priority: -1000, Action: ActionAllow},
		{ID: "maxval", Prefix: "12.0.0.0/8", Protocol: ProtocolTCP, PortStart: 65535, PortEnd: 65535, Action: ActionAllow, Value: make([]byte, 1<<20)},
	}
	for _, r := range ok {
		mustAdd(t, tab, r)
	}
}

func TestStructuralErrorPrecedesSemantic(t *testing.T) {
	tab, err := New(Options{MaxRules: 4, MaxValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, tab, Rule{ID: "dup", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionAllow})
	// Duplicate add comes first, but the structural error later in the
	// batch must win because validation is a separate first phase.
	_, err = tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "dup", Prefix: "10.1.0.0/16", Protocol: ProtocolAny, Action: ActionAllow}},
		{Type: ChangeAdd, Rule: Rule{ID: "bad", Prefix: "nope", Protocol: ProtocolAny, Action: ActionAllow}},
	})
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Fatalf("err=%v", err)
	}
	// Unknown change type and misplaced IDs.
	if _, err = tab.Apply(Change{Type: ChangeType(0)}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("type=%v", err)
	}
	if _, err = tab.Apply(Change{Type: ChangeAdd, ID: "x", Rule: Rule{ID: "y", Prefix: "10.2.0.0/16", Protocol: ProtocolAny, Action: ActionAllow}}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("add-id=%v", err)
	}
	if _, err = tab.Apply(Change{Type: ChangeDelete, ID: "z", Rule: Rule{ID: "y"}}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("del-rule-id=%v", err)
	}
	if _, err = tab.Apply(Change{Type: ChangeDelete, ID: "!!"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("del-id=%v", err)
	}
	if _, err = tab.Apply(Change{Type: ChangeDelete, ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("del-missing=%v", err)
	}
}

func TestCapacityRollbackAndGeneration(t *testing.T) {
	tab, err := New(Options{MaxRules: 2, MaxValueBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	g, err := tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("12")}},
		{Type: ChangeAdd, Rule: Rule{ID: "b", Prefix: "10.1.0.0/16", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("34")}},
	})
	if err != nil || g != 1 {
		t.Fatalf("g=%d err=%v", g, err)
	}
	// Delete-then-add keeps the final state within budget and succeeds.
	g, err = tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		{Type: ChangeAdd, Rule: Rule{ID: "c", Prefix: "10.2.0.0/16", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("56")}},
	})
	if err != nil || g != 2 {
		t.Fatalf("g=%d err=%v", g, err)
	}
	// Exceeding the rule budget in the final state fails and rolls back.
	if _, err = tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "d", Prefix: "10.3.0.0/16", Protocol: ProtocolAny, Action: ActionAllow}},
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("rules=%v", err)
	}
	// Exceeding the value budget in the final state fails and rolls back.
	if _, err = tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "b"},
		{Type: ChangeAdd, Rule: Rule{ID: "e", Prefix: "10.4.0.0/16", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("789")}},
	}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("bytes=%v", err)
	}
	s := tab.Snapshot()
	if s.Generation != 2 || len(s.Rules) != 2 || s.UsedValueBytes != 4 {
		t.Fatalf("snapshot=%+v", s)
	}
	// A no-op rewrite still advances the generation exactly once.
	g, err = tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "c"},
		{Type: ChangeAdd, Rule: Rule{ID: "c", Prefix: "10.2.0.0/16", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("56")}},
	})
	if err != nil || g != 3 {
		t.Fatalf("g=%d err=%v", g, err)
	}
}

func TestMatchPrecedencePortWidthAndPriority(t *testing.T) {
	tab, err := New(Options{MaxRules: 16, MaxValueBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	// Same prefix and protocol: narrower port range wins over priority.
	mustAdd(t, tab, Rule{ID: "wide-hi", Prefix: "10.0.0.0/24", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 1000, Priority: 1000, Action: ActionAllow})
	mustAdd(t, tab, Rule{ID: "narrow-lo", Prefix: "10.0.0.0/24", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Priority: -1000, Action: ActionDeny})
	// Any loses to a specific protocol even with a huge priority edge.
	mustAdd(t, tab, Rule{ID: "any-hi", Prefix: "10.0.0.0/24", Protocol: ProtocolAny, Priority: 1000, Action: ActionAllow})
	d, err := tab.Match("10.0.0.7", ProtocolTCP, 80)
	if err != nil || d.RuleID != "narrow-lo" {
		t.Fatalf("d=%+v err=%v", d, err)
	}
	// UDP only sees the Any rule.
	d, _ = tab.Match("10.0.0.7", ProtocolUDP, 80)
	if d.RuleID != "any-hi" {
		t.Fatalf("udp=%+v", d)
	}
	// Port outside the narrow range falls back to the wide TCP rule.
	d, _ = tab.Match("10.0.0.7", ProtocolTCP, 81)
	if d.RuleID != "wide-hi" {
		t.Fatalf("wide=%+v", d)
	}
	// No candidate: Found=false with the current generation.
	d, _ = tab.Match("192.0.2.1", ProtocolTCP, 80)
	if d.Found || d.Generation != tab.Snapshot().Generation {
		t.Fatalf("miss=%+v", d)
	}
}

func TestIPv6AndMappedNormalization(t *testing.T) {
	tab, err := New(Options{MaxRules: 8, MaxValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(t, tab, Rule{ID: "v6", Prefix: "2001:db8::dead:beef/64", Protocol: ProtocolTCP, PortStart: 0, PortEnd: 65535, Action: ActionAllow})
	d, err := tab.Match("2001:db8::1", ProtocolTCP, 9)
	if err != nil || d.RuleID != "v6" || d.Prefix != "2001:db8::/64" {
		t.Fatalf("v6=%+v err=%v", d, err)
	}
	// A v4-mapped query never matches a native v6 rule and vice versa.
	if d, _ = tab.Match("::ffff:2001:db8::1", ProtocolTCP, 9); d.Found {
		t.Fatalf("mapped matched v6: %+v", d)
	}
	// Zone-bearing match addresses are rejected.
	if _, err = tab.Match("fe80::1%eth0", ProtocolTCP, 1); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("zone=%v", err)
	}
}

func TestSnapshotOrdering(t *testing.T) {
	tab, err := New(Options{MaxRules: 16, MaxValueBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	rules := []Rule{
		{ID: "v6", Prefix: "2001:db8::/32", Protocol: ProtocolAny, Action: ActionAllow},
		{ID: "v4-wide", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionAllow},
		{ID: "v4-narrow", Prefix: "10.0.0.0/16", Protocol: ProtocolAny, Action: ActionAllow},
		{ID: "v4-tcp", Prefix: "10.0.0.0/16", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 2, Action: ActionAllow},
		{ID: "v4-net2", Prefix: "10.1.0.0/16", Protocol: ProtocolAny, Action: ActionAllow},
	}
	for _, r := range rules {
		mustAdd(t, tab, r)
	}
	s := tab.Snapshot()
	var got []string
	for _, rv := range s.Rules {
		got = append(got, rv.ID)
	}
	want := []string{"v4-narrow", "v4-tcp", "v4-wide", "v4-net2", "v6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order=%v want %v", got, want)
	}
}

func TestConcurrentMixedAccess(t *testing.T) {
	tab, err := New(Options{MaxRules: 10000, MaxValueBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("w%d-r%d", w, i%50)
				_, _ = tab.ApplyBatch([]Change{
					{Type: ChangeDelete, ID: id},
					{Type: ChangeAdd, Rule: Rule{ID: id, Prefix: fmt.Sprintf("10.%d.0.0/16", (w*200+i)%256), Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Action: ActionAllow, Value: []byte("v")}},
				})
				_, _ = tab.Match("10.1.0.1", ProtocolTCP, 80)
				snap := tab.Snapshot()
				if snap.UsedValueBytes < 0 {
					t.Errorf("negative used bytes")
				}
			}
		}()
	}
	wg.Wait()
	s := tab.Snapshot()
	if len(s.Rules) > 50 {
		t.Fatalf("rules=%d", len(s.Rules))
	}
}
