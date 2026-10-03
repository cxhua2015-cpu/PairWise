package netpolicy

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func testTable(t *testing.T, rules, bytes int) *Table {
	t.Helper()
	v, err := New(Options{MaxRules: rules, MaxValueBytes: bytes})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func add(t *testing.T, table *Table, rule Rule) uint64 {
	t.Helper()
	g, err := table.Apply(Change{Type: ChangeAdd, Rule: rule})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestNewValidation(t *testing.T) {
	for _, o := range []Options{{}, {MaxRules: 1}, {MaxRules: 10001, MaxValueBytes: 1}, {MaxRules: 1, MaxValueBytes: 64<<20 + 1}} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("opts=%+v err=%v", o, err)
		}
	}
}

func TestCanonicalizationAndStablePrecedence(t *testing.T) {
	tab := testTable(t, 16, 1024)
	_, err := tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "default", Prefix: "10.9.8.7/8", Protocol: ProtocolAny, Action: ActionDeny}},
		{Type: ChangeAdd, Rule: Rule{ID: "wide", Prefix: "10.1.2.99/24", Protocol: ProtocolTCP, PortStart: 400, PortEnd: 500, Priority: 100, Action: ActionDeny}},
		{Type: ChangeAdd, Rule: Rule{ID: "exact", Prefix: "10.1.2.0/24", Protocol: ProtocolTCP, PortStart: 443, PortEnd: 443, Priority: -5, Action: ActionAllow, Value: []byte("ok")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := tab.Match("10.1.2.3", ProtocolTCP, 443)
	if err != nil || !d.Found || d.RuleID != "exact" || d.Prefix != "10.1.2.0/24" || string(d.Value) != "ok" {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
	d, _ = tab.Match("10.2.0.1", ProtocolUDP, 53)
	if d.RuleID != "default" || d.Action != ActionDeny {
		t.Fatalf("fallback=%+v", d)
	}
}

func TestTieBreakDenyThenID(t *testing.T) {
	tab := testTable(t, 8, 128)
	for _, r := range []Rule{
		{ID: "z-allow", Prefix: "192.0.2.0/24", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Priority: 1, Action: ActionAllow},
		{ID: "z-deny", Prefix: "192.0.2.0/24", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Priority: 1, Action: ActionDeny},
		{ID: "a-deny", Prefix: "192.0.2.0/24", Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Priority: 1, Action: ActionDeny},
	} {
		add(t, tab, r)
	}
	d, _ := tab.Match("192.0.2.5", ProtocolTCP, 80)
	if d.RuleID != "a-deny" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestBatchValidationRollbackDeleteReaddAndFinalCapacity(t *testing.T) {
	tab := testTable(t, 1, 3)
	add(t, tab, Rule{ID: "a", Prefix: "0.0.0.0/0", Protocol: ProtocolAny, Action: ActionAllow, Value: []byte("old")})
	_, err := tab.ApplyBatch([]Change{
		{Type: ChangeAdd, Rule: Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionAllow}},
		{Type: ChangeAdd, Rule: Rule{ID: "b", Prefix: "bad", Protocol: ProtocolAny, Action: ActionAllow}},
	})
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Fatalf("precedence=%v", err)
	}
	s := tab.Snapshot()
	if s.Generation != 1 || len(s.Rules) != 1 || string(s.Rules[0].Value) != "old" {
		t.Fatalf("rollback=%+v", s)
	}
	g, err := tab.ApplyBatch([]Change{
		{Type: ChangeDelete, ID: "a"},
		{Type: ChangeAdd, Rule: Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionDeny, Value: []byte("new")}},
	})
	if err != nil || g != 2 {
		t.Fatalf("g=%d err=%v", g, err)
	}
}

func TestMappedIPv4AndIPv6(t *testing.T) {
	tab := testTable(t, 8, 128)
	add(t, tab, Rule{ID: "mapped", Prefix: "::ffff:192.0.2.129/120", Protocol: ProtocolTCP, PortStart: 1, PortEnd: 65535, Action: ActionAllow})
	add(t, tab, Rule{ID: "v6", Prefix: "2001:db8::1234/32", Protocol: ProtocolUDP, PortStart: 53, PortEnd: 53, Action: ActionDeny})
	d, err := tab.Match("::ffff:192.0.2.4", ProtocolTCP, 99)
	if err != nil || d.RuleID != "mapped" || d.Prefix != "192.0.2.0/24" {
		t.Fatalf("mapped=%+v err=%v", d, err)
	}
	d, _ = tab.Match("2001:db8::1", ProtocolUDP, 53)
	if d.RuleID != "v6" || d.Prefix != "2001:db8::/32" {
		t.Fatalf("v6=%+v", d)
	}
}

func TestOwnershipSnapshotAndEmptyBatch(t *testing.T) {
	tab := testTable(t, 8, 128)
	value := []byte("abc")
	add(t, tab, Rule{ID: "b", Prefix: "2001:db8::/32", Protocol: ProtocolAny, Action: ActionAllow, Value: value})
	add(t, tab, Rule{ID: "a", Prefix: "10.0.0.0/8", Protocol: ProtocolAny, Action: ActionAllow})
	value[0] = 'X'
	g, err := tab.ApplyBatch(nil)
	if err != nil || g != 2 {
		t.Fatalf("g=%d err=%v", g, err)
	}
	s1 := tab.Snapshot()
	if len(s1.Rules) != 2 || s1.Rules[0].ID != "a" || string(s1.Rules[1].Value) != "abc" {
		t.Fatalf("snapshot=%+v", s1)
	}
	s1.Rules[1].Value[0] = 'Y'
	if string(tab.Snapshot().Rules[1].Value) != "abc" {
		t.Fatal("snapshot aliases state")
	}
	d, _ := tab.Match("2001:db8::1", ProtocolTCP, 1)
	d.Value[0] = 'Z'
	d2, _ := tab.Match("2001:db8::1", ProtocolTCP, 1)
	if string(d2.Value) != "abc" {
		t.Fatal("decision aliases state")
	}
}

func TestInvalidMatchAndConcurrentCalls(t *testing.T) {
	tab := testTable(t, 128, 4096)
	if _, err := tab.Match("bad", ProtocolTCP, 1); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("address=%v", err)
	}
	if _, err := tab.Match("127.0.0.1", ProtocolAny, 1); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("protocol=%v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("r%02d", i)
			if _, err := tab.Apply(Change{Type: ChangeAdd, Rule: Rule{ID: id, Prefix: fmt.Sprintf("10.%d.0.0/16", i), Protocol: ProtocolTCP, PortStart: 80, PortEnd: 80, Action: ActionAllow}}); err != nil {
				t.Errorf("add: %v", err)
			}
			_, _ = tab.Match(fmt.Sprintf("10.%d.1.1", i), ProtocolTCP, 80)
			_ = tab.Snapshot()
		}()
	}
	wg.Wait()
	if len(tab.Snapshot().Rules) != 32 {
		t.Fatalf("rules=%d", len(tab.Snapshot().Rules))
	}
}
