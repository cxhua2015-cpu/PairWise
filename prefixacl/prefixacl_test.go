package prefixacl

import (
	"errors"
	"fmt"
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

func TestIPv6LongestMatchAndFamilyIsolation(t *testing.T) {
	x := mustTable(t, 8, Deny)
	_, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Deny},
		{Kind: Upsert, Prefix: px("2001:db8:1::/48"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
	}})
	if err != nil {
		t.Fatal(err)
	}
	a, p, f, err := x.Lookup(netip.MustParseAddr("2001:db8:1::1"))
	if err != nil || !f || a != Allow || p != px("2001:db8:1::/48") {
		t.Fatal(a, p, f, err)
	}
	a, p, f, err = x.Lookup(netip.MustParseAddr("2001:db8:2::1"))
	if err != nil || !f || a != Deny || p != px("2001:db8::/32") {
		t.Fatal(a, p, f, err)
	}
	// IPv4 rules must not match IPv6 addresses and vice versa.
	if _, _, f, _ = x.Lookup(netip.MustParseAddr("2001:db9::1")); f {
		t.Fatal("unexpected match outside configured IPv6 prefixes")
	}
	if _, _, f, _ = x.Lookup(netip.MustParseAddr("::ffff:10.1.2.3")); f {
		t.Fatal("IPv4-mapped IPv6 address must not match IPv4 rules")
	}
}

func TestLookupInvalidAddr(t *testing.T) {
	x := mustTable(t, 4, Allow)
	if _, _, _, err := x.Lookup(netip.Addr{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestEmptyBatchChangesNothing(t *testing.T) {
	x := mustTable(t, 4, Deny)
	r, err := x.Apply(Batch{})
	if err != nil || r.Generation != 0 || r.Revision != 0 || r.Changed != nil {
		t.Fatalf("%+v %v", r, err)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 || len(s.Rules) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestValidationErrors(t *testing.T) {
	x := mustTable(t, 4, Deny)
	cases := []Op{
		{Kind: 0, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: 99, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: 0},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: 42},
		{Kind: Delete, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: netip.Prefix{}, Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.1/8"), Action: Allow}, // not canonical
	}
	for i, op := range cases {
		if _, err := x.Apply(Batch{Ops: []Op{op}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if s := x.Snapshot(); s.Generation != 0 || len(s.Rules) != 0 {
		t.Fatalf("failed batches mutated state: %+v", s)
	}
}

func TestCapacityRollbackPreservesRevision(t *testing.T) {
	x := mustTable(t, 1, Deny)
	if _, err := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow}}}); err != nil {
		t.Fatal(err)
	}
	before := x.Snapshot()
	_, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("12.0.0.0/8"), Action: Allow},
	}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	after := x.Snapshot()
	if after.Generation != before.Generation || after.NextRevision != before.NextRevision || len(after.Rules) != 1 {
		t.Fatalf("rollback consumed revision/generation: %+v -> %+v", before, after)
	}
	// The next successful batch must reuse the unconsumed revision.
	r, err := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny}}})
	if err != nil || r.Revision != before.NextRevision {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDeleteNotFoundRollsBackWholeBatch(t *testing.T) {
	x := mustTable(t, 4, Deny)
	_, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Delete, Prefix: px("10.0.0.0/8")},
		{Kind: Delete, Prefix: px("10.0.0.0/8")}, // second delete fails
	}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	s := x.Snapshot()
	if s.Generation != 0 || s.NextRevision != 1 || len(s.Rules) != 0 {
		t.Fatalf("batch not rolled back: %+v", s)
	}
}

func TestChangedDedupAndSnapshotOrder(t *testing.T) {
	x := mustTable(t, 8, Deny)
	r, err := x.Apply(Batch{Ops: []Op{
		{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow},
		{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny}, // duplicate upsert
		{Kind: Upsert, Prefix: px("10.1.0.0/16"), Action: Allow},
		{Kind: Delete, Prefix: px("2001:db8::/32")}, // deleted: not in Changed
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 2 || r.Changed[0].Prefix != px("10.0.0.0/8") || r.Changed[0].Action != Deny ||
		r.Changed[1].Prefix != px("10.1.0.0/16") {
		t.Fatalf("%+v", r.Changed)
	}
	if r.Generation != 1 || r.Revision != 4 {
		t.Fatalf("%+v", r)
	}
}

func TestSnapshotIsolation(t *testing.T) {
	x := mustTable(t, 4, Deny)
	_, _ = x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow}}})
	s := x.Snapshot()
	s.Rules[0].Action = Deny
	if x.Snapshot().Rules[0].Action != Allow {
		t.Fatal("snapshot shares memory with table")
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
			p4 := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i), 0, 0}), 16)
			p6 := netip.PrefixFrom(netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, byte(i)}), 48)
			for j := 0; j < 20; j++ {
				if _, err := x.Apply(Batch{Ops: []Op{
					{Kind: Upsert, Prefix: p4, Action: Allow},
					{Kind: Upsert, Prefix: p6, Action: Deny},
				}}); err != nil {
					t.Error(err)
				}
				_, _, _, _ = x.Lookup(netip.AddrFrom4([4]byte{10, byte(i), 0, 1}))
				_ = x.Snapshot()
			}
			if _, err := x.Apply(Batch{Ops: []Op{{Kind: Delete, Prefix: p6}}}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s := x.Snapshot()
	if len(s.Rules) != 16 {
		t.Fatal(len(s.Rules))
	}
	for _, r := range s.Rules {
		if !r.Prefix.Addr().Is4() {
			t.Fatalf("unexpected survivor: %+v", r)
		}
	}
}

func TestGenerationIncrementsOncePerBatch(t *testing.T) {
	x := mustTable(t, 8, Deny)
	for i := 0; i < 3; i++ {
		r, err := x.Apply(Batch{Ops: []Op{
			{Kind: Upsert, Prefix: px(fmt.Sprintf("10.%d.0.0/16", i)), Action: Allow},
			{Kind: Upsert, Prefix: px(fmt.Sprintf("10.%d.128.0/17", i)), Action: Deny},
		}})
		if err != nil || r.Generation != uint64(i+1) {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if g := x.Snapshot().Generation; g != 3 {
		t.Fatal(g)
	}
}
