package dedupcache

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestInvalidOptions(t *testing.T) {
	valid := Options{MaxEntries: 1, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 4}
	for _, o := range []Options{
		{}, {MaxEntries: 1}, {MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 1},
		{MaxEntries: 1, MaxKeyBytes: 1, MaxTokenBytes: 1, MaxValueBytes: 2, MaxTotalValueBytes: 1},
	} {
		if _, e := New(o); !errors.Is(e, ErrInvalidOptions) {
			t.Fatalf("%+v: %v", o, e)
		}
	}
	if _, e := New(valid); e != nil {
		t.Fatal(e)
	}
}

func TestStructuralValidation(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	bad := []Op{
		{Kind: 0, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 9},
		{Kind: 99, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 9},
		{Kind: Put, Key: "", Token: "t", Value: []byte("x"), ExpiresAt: 9},
		{Kind: Put, Key: "abcde", Token: "t", Value: []byte("x"), ExpiresAt: 9},
		{Kind: Put, Key: "a b", Token: "t", Value: []byte("x"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "", Value: []byte("x"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "t", Value: nil, ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "t", Value: []byte("xxxxx"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 0},
		{Kind: Delete, Key: "a", Token: "t"},
		{Kind: Delete, Key: "a", Value: []byte("x")},
		{Kind: Delete, Key: "a", ExpiresAt: 1},
	}
	for _, op := range bad {
		if _, e := c.Apply(Batch{Ops: []Op{op}}); !errors.Is(e, ErrInvalidInput) {
			t.Fatalf("%+v: %v", op, e)
		}
	}
	if _, e := c.Apply(Batch{Now: -1}); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
}

func TestTimeMonotonicAndRollback(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, e := c.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
	b := c.Snapshot()
	_, e := c.Apply(Batch{Now: 4, Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 9}}})
	if !errors.Is(e, ErrTime) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
	// Equal time is allowed and commits.
	if _, e := c.Apply(Batch{Now: 5}); e != nil {
		t.Fatal(e)
	}
}

func TestDeleteSemantics(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	b := c.Snapshot()
	if _, e := c.Apply(Batch{Ops: []Op{{Kind: Delete, Key: "a"}}}); !errors.Is(e, ErrNotFound) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
	x, e := c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "a", Token: "t", Value: []byte("v"), ExpiresAt: 9},
		{Kind: Delete, Key: "a"},
	}})
	if e != nil || x.Revision != 1 || len(c.Snapshot().Records) != 0 {
		t.Fatalf("%+v %v", x, e)
	}
	// Delete does not allocate a revision; next create reuses the sequence.
	x, e = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "b", Token: "t", Value: []byte("v"), ExpiresAt: 9}}})
	if e != nil || x.Revision != 2 || x.Outcomes[0].Record.Revision != 2 {
		t.Fatalf("%+v %v", x, e)
	}
}

func TestGenerationRules(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	x, _ := c.Apply(Batch{})
	if x.Generation != 0 {
		t.Fatal("empty batch must not increment generation")
	}
	x, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("v"), ExpiresAt: 3}}})
	if x.Generation != 1 {
		t.Fatal(x.Generation)
	}
	// Replay-only batch does not increment generation.
	x, _ = c.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("z"), ExpiresAt: 9}}})
	if x.Generation != 1 || !x.Outcomes[0].Replay {
		t.Fatalf("%+v", x)
	}
	// Pruning alone increments generation once.
	x, _ = c.Apply(Batch{Now: 3})
	if x.Generation != 2 || len(c.Snapshot().Records) != 0 {
		t.Fatalf("%+v", x)
	}
}

func TestGetValidationAndCopy(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	if _, _, e := c.Get(-1, "a"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	if _, _, e := c.Get(0, "bad key"); !errors.Is(e, ErrInvalidInput) {
		t.Fatal(e)
	}
	_, _ = c.Apply(Batch{Now: 2, Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("v"), ExpiresAt: 9}}})
	if _, _, e := c.Get(1, "a"); !errors.Is(e, ErrTime) {
		t.Fatal(e)
	}
	r, ok, e := c.Get(3, "a")
	if e != nil || !ok {
		t.Fatal(e)
	}
	r.Value[0] = 'X'
	if string(c.Snapshot().Records[0].Value) != "v" {
		t.Fatal("Get must return a deep copy")
	}
	if _, ok, _ := c.Get(3, "missing"); ok {
		t.Fatal("missing key")
	}
}

func TestSnapshotIsolation(t *testing.T) {
	c, _ := New(Options{MaxEntries: 4, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 16})
	_, _ = c.Apply(Batch{Ops: []Op{
		{Kind: Put, Key: "b", Token: "t", Value: []byte("2"), ExpiresAt: 9},
		{Kind: Put, Key: "a", Token: "t", Value: []byte("1"), ExpiresAt: 9},
	}})
	s := c.Snapshot()
	if s.Records[0].Key != "a" || s.Records[1].Key != "b" || s.NextRevision != 3 {
		t.Fatalf("%+v", s)
	}
	s.Records[0].Value[0] = 'X'
	if string(c.Snapshot().Records[0].Value) != "1" {
		t.Fatal("Snapshot must be isolated")
	}
}

func TestTotalValueBytesCapacity(t *testing.T) {
	c, _ := New(Options{MaxEntries: 8, MaxKeyBytes: 4, MaxTokenBytes: 4, MaxValueBytes: 4, MaxTotalValueBytes: 5})
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("123"), ExpiresAt: 9}}})
	b := c.Snapshot()
	_, e := c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "b", Token: "t", Value: []byte("456"), ExpiresAt: 9}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
	// Pruning frees capacity: expired records no longer count.
	_, e = c.Apply(Batch{Now: 9, Ops: []Op{{Kind: Put, Key: "b", Token: "t", Value: []byte("456"), ExpiresAt: 19}}})
	if e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentMixed(t *testing.T) {
	c, _ := New(Options{MaxEntries: 128, MaxKeyBytes: 8, MaxTokenBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := fmt.Sprintf("k%d", i%16)
			for n := 0; n < 50; n++ {
				now := int64(n)
				_, _ = c.Apply(Batch{Now: now, Ops: []Op{{Kind: Put, Key: k, Token: "t", Value: []byte("v"), ExpiresAt: now + 100}}})
				_, _, _ = c.Get(now, k)
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
