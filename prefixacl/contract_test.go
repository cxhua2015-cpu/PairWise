package prefixacl

import (
	"errors"
	"net/netip"
	"reflect"
	"sync"
	"testing"
)

func px(s string) netip.Prefix { return netip.MustParsePrefix(s) }
func tab(t *testing.T) *Table {
	t.Helper()
	x, e := New(Options{MaxRules: 4, Default: Deny})
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func TestOptionsAndValidation(t *testing.T) {
	if _, e := New(Options{}); !errors.Is(e, ErrInvalidOptions) {
		t.Fatal(e)
	}
	x := tab(t)
	before := x.Snapshot()
	_, e := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.1/24"), Action: Allow}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}
func TestLongestMatchAndDefault(t *testing.T) {
	x := tab(t)
	_, e := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny}, {Kind: Upsert, Prefix: px("10.1.0.0/16"), Action: Allow}, {Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow}}})
	if e != nil {
		t.Fatal(e)
	}
	a, p, f, e := x.Lookup(netip.MustParseAddr("10.1.2.3"))
	if e != nil || !f || a != Allow || p != px("10.1.0.0/16") {
		t.Fatal(a, p, f, e)
	}
	a, _, f, e = x.Lookup(netip.MustParseAddr("192.0.2.1"))
	if e != nil || f || a != Deny {
		t.Fatal(a, f, e)
	}
}
func TestOrderRevisionAndRollback(t *testing.T) {
	x := tab(t)
	r, e := x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow}, {Kind: Delete, Prefix: px("10.0.0.0/8")}, {Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny}}})
	if e != nil || r.Revision != 2 || len(r.Changed) != 1 || r.Changed[0].Revision != 2 {
		t.Fatalf("%+v %v", r, e)
	}
	before := x.Snapshot()
	_, e = x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow}, {Kind: Delete, Prefix: px("12.0.0.0/8")}}})
	if !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(before, x.Snapshot()) {
		t.Fatal(e)
	}
}
func TestFinalCapacity(t *testing.T) {
	x, _ := New(Options{MaxRules: 1, Default: Deny})
	_, _ = x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Allow}}})
	_, e := x.Apply(Batch{Ops: []Op{{Kind: Delete, Prefix: px("10.0.0.0/8")}, {Kind: Upsert, Prefix: px("11.0.0.0/8"), Action: Allow}}})
	if e != nil || x.Snapshot().Rules[0].Prefix != px("11.0.0.0/8") {
		t.Fatal(e)
	}
}
func TestSnapshotOrder(t *testing.T) {
	x := tab(t)
	_, _ = x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: px("2001:db8::/32"), Action: Allow}, {Kind: Upsert, Prefix: px("10.0.0.0/16"), Action: Allow}, {Kind: Upsert, Prefix: px("10.0.0.0/8"), Action: Deny}}})
	s := x.Snapshot()
	if s.Rules[0].Prefix != px("10.0.0.0/8") || s.Rules[1].Prefix != px("10.0.0.0/16") || !s.Rules[2].Prefix.Addr().Is6() {
		t.Fatalf("%+v", s.Rules)
	}
}
func TestConcurrent(t *testing.T) {
	x, _ := New(Options{MaxRules: 64, Default: Deny})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i), 0, 0}), 16)
			_, _ = x.Apply(Batch{Ops: []Op{{Kind: Upsert, Prefix: p, Action: Allow}}})
			_, _, _, _ = x.Lookup(netip.AddrFrom4([4]byte{10, byte(i), 1, 1}))
			_ = x.Snapshot()
		}()
	}
	wg.Wait()
	if len(x.Snapshot().Rules) != 32 {
		t.Fatal(len(x.Snapshot().Rules))
	}
}
