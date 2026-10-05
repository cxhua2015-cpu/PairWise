package dedupcache

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func cache(t *testing.T) *Cache {
	t.Helper()
	c, e := New(Options{MaxEntries: 3, MaxKeyBytes: 12, MaxTokenBytes: 12, MaxValueBytes: 8, MaxTotalValueBytes: 16})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestValidationBeforeTime(t *testing.T) {
	c := cache(t)
	_, _ = c.Apply(Batch{Now: 5})
	b := c.Snapshot()
	_, e := c.Apply(Batch{Now: 4, Ops: []Op{{Kind: Put, Key: "bad?", Token: "t", Value: []byte("x"), ExpiresAt: 9}}})
	if !errors.Is(e, ErrInvalidInput) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
}
func TestCreateReplayConflict(t *testing.T) {
	c := cache(t)
	x, e := c.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("one"), ExpiresAt: 5}, {Kind: Put, Key: "a", Token: "t", Value: []byte("two"), ExpiresAt: 9}}})
	if e != nil || x.Revision != 1 || !x.Outcomes[0].Created || !x.Outcomes[1].Replay || string(x.Outcomes[1].Record.Value) != "one" {
		t.Fatalf("%+v %v", x, e)
	}
	b := c.Snapshot()
	_, e = c.Apply(Batch{Now: 2, Ops: []Op{{Kind: Put, Key: "a", Token: "x", Value: []byte("z"), ExpiresAt: 9}}})
	if !errors.Is(e, ErrConflict) || !reflect.DeepEqual(b, c.Snapshot()) {
		t.Fatal(e)
	}
}
func TestExpiryBoundaryAndReplace(t *testing.T) {
	c := cache(t)
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 3}}})
	x, e := c.Apply(Batch{Now: 3, Ops: []Op{{Kind: Put, Key: "a", Token: "n", Value: []byte("y"), ExpiresAt: 8}}})
	if e != nil || !x.Outcomes[0].Created || x.Revision != 2 {
		t.Fatal(e)
	}
}
func TestCapacityRollbackAndOwnership(t *testing.T) {
	c, _ := New(Options{MaxEntries: 1, MaxKeyBytes: 8, MaxTokenBytes: 8, MaxValueBytes: 4, MaxTotalValueBytes: 4})
	v := []byte("abc")
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: v, ExpiresAt: 5}}})
	v[0] = 'z'
	b := c.Snapshot()
	_, e := c.Apply(Batch{Now: 1, Ops: []Op{{Kind: Put, Key: "b", Token: "t", Value: []byte("x"), ExpiresAt: 5}}})
	if !errors.Is(e, ErrCapacity) || !reflect.DeepEqual(b, c.Snapshot()) || string(c.Snapshot().Records[0].Value) != "abc" {
		t.Fatal(e)
	}
}
func TestGetPrunesGlobally(t *testing.T) {
	c := cache(t)
	_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: "a", Token: "t", Value: []byte("x"), ExpiresAt: 2}, {Kind: Put, Key: "b", Token: "t", Value: []byte("y"), ExpiresAt: 4}}})
	_, f, e := c.Get(2, "b")
	if e != nil || !f || len(c.Snapshot().Records) != 1 {
		t.Fatal(e)
	}
}
func TestConcurrent(t *testing.T) {
	c, _ := New(Options{MaxEntries: 64, MaxKeyBytes: 8, MaxTokenBytes: 8, MaxValueBytes: 2, MaxTotalValueBytes: 128})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k := string(rune('a' + i))
			_, _ = c.Apply(Batch{Ops: []Op{{Kind: Put, Key: k, Token: "t", Value: []byte("v"), ExpiresAt: 9}}})
			_, _, _ = c.Get(0, k)
			_ = c.Snapshot()
		}()
	}
	wg.Wait()
	if len(c.Snapshot().Records) != 24 {
		t.Fatal(len(c.Snapshot().Records))
	}
}
