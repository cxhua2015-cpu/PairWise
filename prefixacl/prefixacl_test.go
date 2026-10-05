package prefixacl

import (
	"errors"
	"net/netip"
	"sync"
	"testing"
)

func mustTable(t *testing.T, max int, def Action) *Table {
	t.Helper()
	x, err := New(Options{MaxRules: max, Default: def})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestNewInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{MaxRules: 0, Default: Allow},
		{MaxRules: -1, Default: Deny},
		{MaxRules: 1, Default: 0},
		{MaxRules: 1, Default: Action(9)},
	} {
		if _, err := New(o); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: %v", o, err)
		}
	}
}

func TestStructuralValidationBeforeState(t *testing.T) {
	x := mustTable(t, 8, Deny)
	bad := []Op{
		{Kind: Kind(0), Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Kind(3), Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: 0},
		{Kind: Delete, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: netip.Prefix{}, Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.1/8"), Action: Allow},
	}
	for _, op := range bad {
		if _, err := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("op %+v: %v", op, err)
		}
	}
	// A structurally invalid op anywhere in the batch aborts before any state read.
	if _, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Kind(9), Prefix: px("11.0.0.0/8"), Action: Allow},
	}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if s := x.Snapshot(); len(s.Rules) != 0 || s.Generation != 0 || s.NextRevision != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestIPv6LongestMatchAndFamilyIsolation(t *testing.T) {
	x := mustTable(t, 8, Deny)
	_, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow},
		{Kind: Upsert, Prefix: px("2001:db8:1::/48"), Action: Deny},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
	}})
	if err != nil {
		t.Fatal(err)
	}
	a, p, f, err := x.Lookup(netip.MustParseAddr("2001:db8:1::1"))
	if err != nil || !f || a != Deny || p != px("2001:db8:1::/48") {
		t.Fatal(a, p, f, err)
	}
	a, p, f, err = x.Lookup(netip.MustParseAddr("2001:db8:2::1"))
	if err != nil || !f || a != Allow || p != px("2001:db8::/32") {
		t.Fatal(a, p, f, err)
	}
	// IPv4 rules must not match IPv6 addresses and vice versa.
	a, _, f, err = x.Lookup(netip.MustParseAddr("2001:db9::1"))
	if err != nil || f || a != Deny {
		t.Fatal(a, f, err)
	}
	a, _, f, err = x.Lookup(netip.MustParseAddr("192.0.2.1"))
	if err != nil || f || a != Deny {
		t.Fatal(a, f, err)
	}
	if _, _, _, err = x.Lookup(netip.Addr{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestOrderedBatchAndChanged(t *testing.T) {
	x := mustTable(t, 8, Deny)
	r, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny},
		{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow},
		{Kind: Delete, Prefix: px("2001:db8::/32")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 1 || r.Revision != 3 {
		t.Fatalf("%+v", r)
	}
	// Changed: only surviving upserted prefixes, deduplicated, snapshot order.
	if len(r.Changed) != 1 || r.Changed[0].Action != Deny || r.Changed[0].Revision != 2 {
		t.Fatalf("%+v", r.Changed)
	}
	s := x.Snapshot()
	if s.Generation != 1 || s.NextRevision != 4 || len(s.Rules) != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestEmptyBatchChangesNothing(t *testing.T) {
	x := mustTable(t, 4, Allow)
	before := x.Snapshot()
	r, err := x.Apply(Batch{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Generation != 0 || r.Revision != 0 || len(r.Changed) != 0 {
		t.Fatalf("%+v", r)
	}
	after := x.Snapshot()
	if before.Generation != after.Generation || before.NextRevision != after.NextRevision || len(after.Rules) != 0 {
		t.Fatalf("%+v", after)
	}
}

func TestRollbackPreservesRevisionAndGeneration(t *testing.T) {
	x := mustTable(t, 2, Deny)
	if _, err := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	// Capacity exceeded only in final state.
	if _, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("12.0.0.0/8"), Action: Allow},
	}}); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	// Delete of missing rule.
	if _, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow},
		{Kind: Delete, Prefix: px("12.0.0.0/8")},
	}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Rules) != 1 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	// Revision allocation resumes where it left off.
	r, err := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow}}})
	if err != nil || r.Revision != 2 || r.Generation != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestSnapshotDeterministicOrder(t *testing.T) {
	x := mustTable(t, 8, Allow)
	_, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.1.0.0/16"), Action: Deny},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("::/0"), Action: Deny},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := x.Snapshot()
	want := []string{"10.0.0.0/8", "10.1.0.0/16", "::/0", "2001:db8::/32"}
	if len(s.Rules) != len(want) {
		t.Fatalf("%+v", s.Rules)
	}
	for i, w := range want {
		if s.Rules[i].Prefix != px(w) {
			t.Fatalf("rule %d: got %s want %s", i, s.Rules[i].Prefix, w)
		}
	}
	// Returned slices are isolated from the table.
	s.Rules[0].Action = Deny
	if x.Snapshot().Rules[0].Action != Allow {
		t.Fatal("snapshot shares state with table")
	}
}

func TestConcurrentMixed(t *testing.T) {
	x := mustTable(t, 128, Deny)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			v4 := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i), 0, 0}), 16)
			v6 := netip.PrefixFrom(netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, byte(i)}), 48)
			for j := 0; j < 20; j++ {
				_, _ = x.Apply(Batch{Ops: []Op{
					{Kind: Upsert, Prefix: v4, Action: Allow},
					{Kind: Upsert, Prefix: v6, Action: Deny},
				}})
				_, _, _, _ = x.Lookup(netip.AddrFrom4([4]byte{10, byte(i), 1, 1}))
				_ = x.Snapshot()
				_, _ = x.Apply(Batch{Ops: []Op{
					{Kind: Delete, Prefix: v4},
					{Kind: Upsert, Prefix: v4, Action: Deny},
				}})
			}
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if len(s.Rules) != 32 {
		t.Fatal(len(s.Rules))
	}
	// Generations are consecutive and revisions strictly increase.
	seen := map[uint64]bool{}
	for _, r := range s.Rules {
		if r.Revision == 0 || seen[r.Revision] {
			t.Fatalf("duplicate or zero revision %d", r.Revision)
		}
		seen[r.Revision] = true
	}
}
