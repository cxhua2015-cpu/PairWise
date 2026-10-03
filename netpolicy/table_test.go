package netpolicy

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestRuleFieldValidationBoundaries(t *testing.T) {
	tab := testTable(t, 64, 1<<20)
	base := Rule{ID: "ok", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 2, Action: ActionAllow}
	cases := []struct {
		mutate func(*Rule)
		want   error
	}{
		{func(r *Rule) { r.ID = "" }, ErrInvalidID},
		{func(r *Rule) { r.ID = strings.Repeat("a", 65) }, ErrInvalidID},
		{func(r *Rule) { r.ID = "bad id" }, ErrInvalidID},
		{func(r *Rule) { r.ID = "bad/id" }, ErrInvalidID},
		{func(r *Rule) { r.ID = "bad:id" }, ErrInvalidID},
		{func(r *Rule) { r.Prefix = "10.0.0.0/33" }, ErrInvalidPrefix},
		{func(r *Rule) { r.Prefix = "fe80::1%eth0/64" }, ErrInvalidPrefix},
		{func(r *Rule) { r.Protocol = Protocol(9) }, ErrInvalidProtocol},
		{func(r *Rule) { r.Protocol = ProtocolAny; r.PortEnd = 1 }, ErrInvalidPort},
		{func(r *Rule) { r.PortStart = 5; r.PortEnd = 4 }, ErrInvalidPort},
		{func(r *Rule) { r.Priority = 1001 }, ErrInvalidPriority},
		{func(r *Rule) { r.Priority = -1001 }, ErrInvalidPriority},
		{func(r *Rule) { r.Action = Action(7) }, ErrInvalidAction},
		{func(r *Rule) { r.Value = make([]byte, 1<<20+1) }, ErrValueTooLarge},
	}
	for i, c := range cases {
		r := base
		c.mutate(&r)
		if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: r}); !errors.Is(err, c.want) {
			t.Fatalf("case %d: got %v want %v", i, err, c.want)
		}
	}
	// Boundary-valid values succeed.
	for i, r := range []Rule{
		{ID: strings.Repeat("a", 64), Prefix: "0.0.0.0/0", Protocol: ProtocolAny, Action: ActionAllow},
		{ID: "p0", Prefix: "::/0", Protocol: ProtocolTCP, PortStart: 0, PortEnd: 0, Priority: -1000, Action: ActionDeny},
		{ID: "p1", Prefix: "10.0.0.0/8", Protocol: ProtocolUDP, PortStart: 65535, PortEnd: 65535, Priority: 1000, Action: ActionAllow, Value: make([]byte, 1<<20)},
	} {
		if i == 2 {
			tab = testTable(t, 64, 1<<20)
		}
		if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: r}); err != nil {
			t.Fatalf("boundary rule %d: %v", i, err)
		}
	}
}

func TestChangeShapeValidation(t *testing.T) {
	tab := testTable(t, 8, 128)
	if _, err := tab.Apply(Change{Type: ChangeType(0)}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("type0=%v", err)
	}
	if _, err := tab.Apply(Change{Type: ChangeAdd, ID: "x", Rule: Rule{ID: "y", Prefix: "10.0.0.0/8", Protocol: ProtocolAny}}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("add-with-id=%v", err)
	}
	if _, err := tab.Apply(Change{Type: ChangeDelete, ID: "x", Rule: Rule{ID: "y"}}); !errors.Is(err, ErrInvalidChange) {
		t.Fatalf("delete-with-rule-id=%v", err)
	}
	if _, err := tab.Apply(Change{Type: ChangeDelete, ID: "bad id"}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("delete-bad-id=%v", err)
	}
	if _, err := tab.Apply(Change{Type: ChangeDelete, ID: "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete-missing=%v", err)
	}
	// Structural error in a later change beats duplicate ID in an earlier one.
	add(t, tab, Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny})
	_, err := tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny}},
		{Type: ChangeAdd, Rule: Rule{ID: "b", Prefix: "nope", Protocol: ProtocolAny}},
	})
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Fatalf("precedence=%v", err)
	}
	if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny}}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("dup=%v", err)
	}
}

func TestCapacityCheckedOnlyAtEnd(t *testing.T) {
	tab := testTable(t, 2, 4)
	add(t, tab, Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Value: []byte("12")})
	add(t, tab, Rule{ID: "b", Prefix: "10.1.0.0/16", Protocol: ProtocolAny, Value: []byte("34")})
	// Delete-then-add stays within budget only in the final state.
	g, err := tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		{Type: ChangeDelete, ID: "b"},
		{Type: ChangeAdd, Rule: Rule{ID: "c", Prefix: "10.2.0.0/16", Protocol: ProtocolAny, Value: []byte("abcd")}},
	})
	if err != nil || g != 3 {
		t.Fatalf("g=%d err=%v", g, err)
	}
	// Final state over budget rolls back everything.
	_, err = tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "c"},
		{Type: ChangeAdd, Rule: Rule{ID: "d", Prefix: "10.3.0.0/16", Protocol: ProtocolAny, Value: []byte("abcde")}},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity=%v", err)
	}
	s := tab.Snapshot()
	if s.Generation != 3 || len(s.Rules) != 1 || s.Rules[0].ID != "c" || s.UsedValueBytes != 4 {
		t.Fatalf("rollback=%+v", s)
	}
	// Too many rules in final state.
	_, err = tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "e", Prefix: "10.4.0.0/16", Protocol: ProtocolAny}},
		{Type: ChangeAdd, Rule: Rule{ID: "f", Prefix: "10.5.0.0/16", Protocol: ProtocolAny}},
	})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("rule-capacity=%v", err)
	}
}

func TestMatchPrecedenceChain(t *testing.T) {
	tab := testTable(t, 16, 256)
	rules := []Rule{
		{ID: "long-prefix", Prefix: "10.0.0.0/9", Protocol: ProtocolAny, Priority: -1000, Action: ActionAllow},
		{ID: "specific-proto", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 0, PortEnd: 65535, Action: ActionAllow},
		{ID: "narrow-port", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Priority: 1000, Action: ActionAllow},
		{ID: "high-prio", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Priority: 500, Action: ActionAllow},
	}
	for _, r := range rules {
		add(t, tab, r)
	}
	// Longest prefix wins over everything else.
	if d, _ := tab.Match("10.0.0.1", ProtocolTCP, 80); d.RuleID != "long-prefix" {
		t.Fatalf("prefix=%+v", d)
	}
	// Same prefix: specific protocol beats Any.
	if d, _ := tab.Match("10.128.0.1", ProtocolTCP, 80); d.RuleID != "specific-proto" {
		t.Fatalf("proto=%+v", d)
	}
	// UDP falls to Any rules; narrower port width (0..0 vs implicit 65536) — both Any,
	// so priority decides between narrow-port(1000) and high-prio(500).
	if d, _ := tab.Match("10.128.0.1", ProtocolUDP, 9); d.RuleID != "narrow-port" {
		t.Fatalf("priority=%+v", d)
	}
	// Port width tiebreak among specific protocols.
	tab2 := testTable(t, 8, 128)
	add(t, tab2, Rule{ID: "wide", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 100, Priority: 999, Action: ActionAllow})
	add(t, tab2, Rule{ID: "narrow", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 50, PortEnd: 60, Priority: -999, Action: ActionAllow})
	if d, _ := tab2.Match("10.0.0.1", ProtocolTCP, 55); d.RuleID != "narrow" {
		t.Fatalf("width=%+v", d)
	}
}

func TestMatchAddressForms(t *testing.T) {
	tab := testTable(t, 8, 128)
	add(t, tab, Rule{ID: "v4", Prefix: "192.0.2.0/24", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Action: ActionAllow})
	add(t, tab, Rule{ID: "v6", Prefix: "2001:db8::/32", Protocol: ProtocolUDP, PortStart: 53, PortEnd: 53, Action: ActionDeny})
	// Mapped form matches the IPv4 rule.
	if d, err := tab.Match("::ffff:192.0.2.7", ProtocolTCP, 80); err != nil || d.RuleID != "v4" {
		t.Fatalf("mapped=%+v err=%v", d, err)
	}
	// v4 rule must not match v6 traffic and vice versa.
	if d, _ := tab.Match("2001:db8::1", ProtocolTCP, 80); d.Found {
		t.Fatalf("cross-family=%+v", d)
	}
	if d, _ := tab.Match("192.0.2.7", ProtocolUDP, 53); d.Found {
		t.Fatalf("cross-family=%+v", d)
	}
	// Zone addresses and garbage are rejected.
	for _, bad := range []string{"fe80::1%eth0", "10.0.0.256", "::ffff:999.1.1.1", ""} {
		if _, err := tab.Match(bad, ProtocolTCP, 1); !errors.Is(err, ErrInvalidAddress) {
			t.Fatalf("addr %q: %v", bad, err)
		}
	}
	if _, err := tab.Match("2001:db8::1", Protocol(3), 1); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("proto=%v", err)
	}
	// No candidate: Found=false with current generation.
	d, err := tab.Match("203.0.113.1", ProtocolTCP, 1)
	if err != nil || d.Found || d.Generation != 2 {
		t.Fatalf("empty=%+v err=%v", d, err)
	}
}

func TestSnapshotOrdering(t *testing.T) {
	tab := testTable(t, 16, 256)
	rules := []Rule{
		{ID: "v6", Prefix: "2001:db8::/32", Protocol: ProtocolAny},
		{ID: "v4-b", Prefix: "10.1.0.0/16", Protocol: ProtocolAny},
		{ID: "v4-a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny},
		{ID: "v4-a-longer", Prefix: "10.0.0.0/9", Protocol: ProtocolAny},
		{ID: "v4-a-tcp", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80},
		{ID: "v4-a-tcp2", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 81, PortEnd: 81, Priority: 5},
	}
	for _, r := range rules {
		add(t, tab, r)
	}
	var got []string
	for _, rv := range tab.Snapshot().Rules {
		got = append(got, rv.ID)
	}
	want := []string{"v4-a-longer", "v4-a", "v4-a-tcp", "v4-a-tcp2", "v4-b", "v6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order=%v want %v", got, want)
	}
}

func TestInputValueIsolation(t *testing.T) {
	tab := testTable(t, 8, 128)
	v := []byte("hello")
	add(t, tab, Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 1, Value: v})
	for i := range v {
		v[i] = 'x'
	}
	d, _ := tab.Match("10.0.0.1", ProtocolTCP, 1)
	if string(d.Value) != "hello" {
		t.Fatalf("input aliased: %q", d.Value)
	}
	// Nil value round-trips as empty.
	add(t, tab, Rule{ID: "b", Prefix: "10.1.0.0/16", Protocol: ProtocolTCP, PortStart: 2, PortEnd: 2})
	d, _ = tab.Match("10.1.0.1", ProtocolTCP, 2)
	if len(d.Value) != 0 {
		t.Fatalf("nil value=%q", d.Value)
	}
}

func TestConcurrentMixed(t *testing.T) {
	tab := testTable(t, 10000, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("w%02d", i)
			for n := 0; n < 50; n++ {
				_, _ = tab.ApplyBatch([]Change{
					{Type: ChangeAdd, Rule: Rule{ID: id, Prefix: fmt.Sprintf("10.%d.0.0/16", i), Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Action: ActionAllow, Value: []byte{byte(i)}}},
				})
				_, _ = tab.Match(fmt.Sprintf("10.%d.1.1", i), ProtocolTCP, 80)
				s := tab.Snapshot()
				for _, rv := range s.Rules {
					rv.Value = append(rv.Value, 0)
				}
				_, _ = tab.ApplyBatch([]Change{{Type: ChangeDelete, ID: id}})
			}
		}()
	}
	wg.Wait()
	if len(tab.Snapshot().Rules) != 0 {
		t.Fatal("expected empty table")
	}
}
