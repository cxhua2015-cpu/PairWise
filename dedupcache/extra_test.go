package dedupcache

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	base := Options{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1}
	if _, e := New(base); e != nil {
		t.Fatal(e)
	}
	for _, o := range []Options{
		{MaxEntries: 0, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxEntries: 1, MaxKeyBytes: 0, MaxTokenBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 0, MaxValueBytes: 1, MaxTotalValueBytes: 1},
		{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 0, MaxTotalValueBytes: 1},
		{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 1, MaxTotalValueBytes: 0},
		{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 2, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
}

func TestStructuralValidation(t *testing.T) {
	c := cache(t)
	put := Op{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 9}
	bad := []Batch{
		{Now: -1},
		{Ops: []Op{{Kind: 0, Key: "a"}}},
		{Ops: []Op{{Kind: 99, Key: "a"}}},
		{Ops: []Op{{Kind: Put, Key: "", Token: "t", Value: []byte("x"), ExpiresAt: 9}}},
		{Ops: []Op{{Kind: Put, Key: "a b", Token: "t", Value: []byte("x"), ExpiresAt: 9}}},
		{Ops: []Op{{Kind: Put, Key: "a", Token: "", Value: []byte("x"), ExpiresAt: 9}}},
		{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: nil, ExpiresAt: 9}}},
		{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("012345678"), ExpiresAt: 9}}},
		{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 0}}},
		{Ops: []Op{{Kind: Delete, Key: "a", Token: "t"}}},
		{Ops: []Op{{Kind: Delete, Key: "a", Value: []byte("x")}}},
		{Ops: []Op{{Kind: Delete, Key: "a", ExpiresAt: 1}}},
	}
	for _, b := range bad {
		if _, e := c.Apply(b); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", b, e)
		}
	}
	if _, e := c.Apply(Batch{Ops: []Op{put}}); e != nil {
		t.Fatal(e)
	}
	long := string(make([]byte, 13))
	for i := range long {
		long = long[:i] + "a" + long[i+1:]
	}
	if _, e := c.Apply(Batch{Ops: []Op{{Kind: Put, Key: long, Token: "t", Value: []byte("x"), ExpiresAt: 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "k", Token: long, Value: []byte("x"), ExpiresAt: 9}}}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicityAndRollback(t *testing.T) {
	c := cache(t)
	if _, e := c.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	b := c.Snapshot()
	if _, e := c.Apply(Batch{Now: 4}); !errors.Is(e, ErrTime) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
	if _, _, e := c.Get(4, "a"); !errors.Is(e, ErrTime) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
	if _, _, e := c.Get(-1, "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := c.Get(9, "bad?"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	c := cache(t)
	if _, e := c.Apply(Batch{Ops: []Op{{Kind: Delete, Key: "a"}}}); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	x, e := c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "a", Token: "t", Value: []byte("v"), ExpiresAt: 9},
		{Kind: Delete, Key: "a"},
	}})
	if e != nil || x.Revision != 1 || x.Generation != 1 || len(c.Snapshot().Records) != 0 {
		t.Fatalf("%+v %v", x, e)
	}
	x, e = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t2", Value: []byte("w"), ExpiresAt: 9}}})
	if e != nil || x.Revision != 2 || !x.Outcomes[0].Created {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestGenerationRules(t *testing.T) {
	c := cache(t)
	x, _ := c.Apply(Batch{})
	if x.Generation != 0 {
		t.Fatal(x)
	}
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 5}}})
	x, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 5}}})
	if x.Generation != 1 {
		t.Fatal(x)
	}
	x, _ = c.Apply(Batch{Now: 5})
	if x.Generation != 2 {
		t.Fatal(x)
	}
	x, _ = c.Apply(Batch{Now: 6})
	if x.Generation != 2 {
		t.Fatal(x)
	}
}

func TestTotalValueBytesCapacity(t *testing.T) {
	c, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 8, MaxTokenBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 6})
	_, e := c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "a", Token: "t", Value: []byte("1234"), ExpiresAt: 9},
		{Kind: Put, Key: "b", Token: "t", Value: []byte("1234"), ExpiresAt: 9},
	}})
	if !errors.Is(e, ErrCapacity) || len(c.Snapshot().Records) != 0 {
		t.Fatal(e)
	}
	if _, e := c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "a", Token: "t", Value: []byte("1234"), ExpiresAt: 9},
		{Kind: Put, Key: "b", Token: "t", Value: []byte("12"), ExpiresAt: 9},
	}}); e != nil {
		t.Fatal(e)
	}
}

func TestExpiryPruningFreesCapacity(t *testing.T) {
	c, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8, MaxTokenBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 2}}})
	x, e := c.Apply(Batch{Now: 2, Ops: []Op{{Kind: Put, Key: "b", Token: "t", Value: []byte("y"), ExpiresAt: 9}}})
	if e != nil || !x.Outcomes[0].Created {
		t.Fatalf("%+v %v", x, e)
	}
	s := c.Snapshot()
	if len(s.Records) != 1 || s.Records[0].Key != "b" {
		t.Fatal(s)
	}
}

func TestDeepCopyIsolation(t *testing.T) {
	c := cache(t)
	v := []byte("abc")
	x, _ := c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: v, ExpiresAt: 9}}})
	v[0] = 'z'
	x.Outcomes[0].Record.Value[1] = 'z'
	r, f, _ := c.Get(0, "a")
	if !f || string(r.Value) != "abc" {
		t.Fatal(r)
	}
	r.Value[0] = 'q'
	s := c.Snapshot()
	if string(s.Records[0].Value) != "abc" {
		t.Fatal(s)
	}
	s.Records[0].Value[0] = 'q'
	if string(c.Snapshot().Records[0].Value) != "abc" {
		t.Fatal("snapshot alias")
	}
}

func TestGetAdvancesTimeAndExpiresBoundary(t *testing.T) {
	c := cache(t)
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 3}}})
	if _, f, _ := c.Get(2, "a"); !f {
		t.Fatal("should be live at 2")
	}
	if _, f, _ := c.Get(3, "a"); f {
		t.Fatal("should expire at ExpiresAt")
	}
	if c.Snapshot().Now != 3 {
		t.Fatal("time not advanced")
	}
}

func TestSnapshotOrdering(t *testing.T) {
	c := cache(t)
	_, _ = c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "c", Token: "t", Value: []byte("1"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "t", Value: []byte("1"), ExpiresAt: 9},
		{Kind: Put, Key: "b", Token: "t", Value: []byte("1"), ExpiresAt: 9},
	}})
	s := c.Snapshot()
	if s.NextRevision != 4 || s.Records[0].Key != "a" || s.Records[1].Key != "b" || s.Records[2].Key != "c" {
		t.Fatal(s)
	}
}

func TestConcurrentMixed(t *testing.T) {
	c, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 16, MaxTokenBytes: 16, MaxValueBytes: 8, MaxTotalValueBytes: 1024})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("key-%02d", i%16)
			tok := fmt.Sprintf("tok-%d", i%2)
			for n := int64(0); n < 20; n++ {
				_, _ = c.Apply(Batch{Now: n, Ops: []Op{{Kind: Put, Key: k, Token: tok, Value: []byte("v"), ExpiresAt: 1000}}})
				_, _, _ = c.Get(n, k)
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	s := c.Snapshot()
	if len(s.Records) == 0 || len(s.Records) > 16 {
		t.Fatal(len(s.Records))
	}
}
