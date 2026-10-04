package breaker

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		pols []Policy
	}{
		{"zero services", Options{MaxServices: 0, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"zero name bytes", Options{MaxServices: 1, MaxNameBytes: 0}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"no policies", Options{MaxServices: 1, MaxNameBytes: 4}, nil},
		{"too many", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}, {Name: "b", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"empty name", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"long name", Options{MaxServices: 1, MaxNameBytes: 1}, []Policy{{Name: "ab", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"bad char", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a?", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"zero failure threshold", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 0, RecoveryThreshold: 1, OpenFor: 1}}},
		{"zero recovery threshold", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 0, OpenFor: 1}}},
		{"zero open for", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 0}}},
		{"negative open for", Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: -1}}},
	}
	for _, tc := range cases {
		if _, err := New(tc.opts, tc.pols); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
	}
	if _, err := New(Options{MaxServices: 2, MaxNameBytes: 16}, []Policy{{Name: "a.b/c_d-e", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationSemantics(t *testing.T) {
	r := reg(t)
	if g := r.Snapshot().Generation; g != 0 {
		t.Fatalf("initial generation=%d", g)
	}
	// Empty batch, no transitions: no increment.
	if _, err := r.Record(Batch{Now: 1}); err != nil {
		t.Fatal(err)
	}
	if g := r.Snapshot().Generation; g != 0 {
		t.Fatalf("empty batch generation=%d", g)
	}
	// Nonempty successful batch increments once.
	res, err := r.Record(Batch{Now: 2, Events: []Event{ev("api", true), ev("db", true)}})
	if err != nil || res.Generation != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// Failed batch does not increment.
	if _, err := r.Record(Batch{Now: 2, Events: []Event{ev("nope", true)}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if g := r.Snapshot().Generation; g != 1 {
		t.Fatalf("after failure generation=%d", g)
	}
	// Allow/Sweep without transitions do not increment.
	if _, _, err := r.Allow("api", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sweep(4); err != nil {
		t.Fatal(err)
	}
	if g := r.Snapshot().Generation; g != 1 {
		t.Fatalf("after allow/sweep generation=%d", g)
	}
	// Open db, then an empty batch that triggers an automatic transition
	// increments generation once.
	if _, err := r.Record(Batch{Now: 4, Events: []Event{ev("db", false)}}); err != nil {
		t.Fatal(err)
	}
	res, err = r.Record(Batch{Now: 7})
	if err != nil || res.Generation != 3 || !reflect.DeepEqual(res.Changed, []string{"db"}) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestMultipleTransitionsSameBatch(t *testing.T) {
	r := reg(t)
	// api: open at now=1 (threshold 2, OpenFor 5 -> OpenUntil 6).
	if _, err := r.Record(Batch{Now: 1, Events: []Event{ev("api", false), ev("api", false)}}); err != nil {
		t.Fatal(err)
	}
	// At now=6 api auto-advances to HalfOpen, then two successes close it.
	res, err := r.Record(Batch{Now: 6, Events: []Event{ev("api", true), ev("api", true)}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Changed, []string{"api"}) {
		t.Fatalf("changed=%v", res.Changed)
	}
	s := r.Snapshot().Services[0]
	if s.Mode != Closed || s.Failures != 0 || s.RecoverySuccesses != 0 {
		t.Fatalf("s=%+v", s)
	}
}

func TestHalfOpenReopenInSameBatch(t *testing.T) {
	r := reg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{ev("db", false)}}); err != nil {
		t.Fatal(err)
	}
	// db opens until 4. At now=4: auto HalfOpen, failure reopens until 7.
	res, err := r.Record(Batch{Now: 4, Events: []Event{ev("db", false)}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Changed, []string{"db"}) {
		t.Fatalf("changed=%v", res.Changed)
	}
	for _, s := range r.Snapshot().Services {
		if s.Name == "db" && (s.Mode != Open || s.OpenUntil != 7) {
			t.Fatalf("s=%+v", s)
		}
	}
}

func TestRollbackPreservesAutoTransitions(t *testing.T) {
	r := reg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{ev("db", false)}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	// At now=10 db would auto-advance to HalfOpen, but the batch fails;
	// the automatic transition, time, and generation must not leak.
	if _, err := r.Record(Batch{Now: 10, Events: []Event{ev("ghost", true)}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, r.Snapshot()) {
		t.Fatalf("state leaked: %+v", r.Snapshot())
	}
	// Time also did not advance.
	if _, err := r.Record(Batch{Now: 5, Events: []Event{ev("api", true)}}); err != nil {
		t.Fatalf("time leaked: %v", err)
	}
}

func TestClosedFailureResetBySuccess(t *testing.T) {
	r := reg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{ev("api", false), ev("api", true), ev("api", false)}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot().Services[0]
	if s.Mode != Closed || s.Failures != 1 {
		t.Fatalf("s=%+v", s)
	}
}

func TestCrossServiceIsolation(t *testing.T) {
	r := reg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{ev("db", false)}}); err != nil {
		t.Fatal(err)
	}
	ok, s, err := r.Allow("api", 1)
	if err != nil || !ok || s.Mode != Closed {
		t.Fatalf("ok=%v s=%+v err=%v", ok, s, err)
	}
	ok, s, err = r.Allow("db", 1)
	if err != nil || ok || s.Mode != Open {
		t.Fatalf("ok=%v s=%+v err=%v", ok, s, err)
	}
}

func TestAllowValidationAndUnknown(t *testing.T) {
	r := reg(t)
	if _, _, err := r.Allow("bad?", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, _, err := r.Allow("ghost", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := r.Allow("", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestExtremeTimes(t *testing.T) {
	r, err := New(Options{MaxServices: 1, MaxNameBytes: 4}, []Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: math.MaxInt64}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(Batch{Now: math.MaxInt64, Events: []Event{ev("x", false)}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot().Services[0]
	if s.Mode != Open || s.OpenUntil != math.MaxInt64 {
		t.Fatalf("s=%+v", s)
	}
	// OpenUntil saturated at MaxInt64: now >= OpenUntil holds, so it advances.
	changed, err := r.Sweep(math.MaxInt64)
	if err != nil || !reflect.DeepEqual(changed, []string{"x"}) {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	// Any earlier time is rejected.
	if _, err := r.Sweep(math.MaxInt64 - 1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestNegativeNowRejected(t *testing.T) {
	r := reg(t)
	if _, err := r.Record(Batch{Now: -1, Events: []Event{ev("api", true)}}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, _, err := r.Allow("api", -1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := r.Sweep(-1); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
}

func TestSnapshotSortedAndStable(t *testing.T) {
	r, err := New(Options{MaxServices: 4, MaxNameBytes: 8}, []Policy{
		{Name: "zeta", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1},
		{Name: "alpha", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1},
		{Name: "mid", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := r.Snapshot()
	names := []string{snap.Services[0].Name, snap.Services[1].Name, snap.Services[2].Name}
	if !reflect.DeepEqual(names, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("names=%v", names)
	}
	// Mutating the returned snapshot must not affect the registry.
	snap.Services[0].Mode = Open
	if r.Snapshot().Services[0].Mode != Closed {
		t.Fatal("snapshot aliases registry state")
	}
}

func TestChangedSortedNoDuplicates(t *testing.T) {
	r := reg(t)
	// db opens on its first failure; a second db failure in the same batch
	// would hit Open and fail, so each service transitions exactly once here.
	res, err := r.Record(Batch{Now: 1, Events: []Event{
		ev("db", false), ev("api", false), ev("api", false),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Changed, []string{"api", "db"}) {
		t.Fatalf("changed=%v", res.Changed)
	}
}

func TestConcurrentMixed(t *testing.T) {
	r := reg(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := int64(0); n < 50; n++ {
				_, _, _ = r.Allow("api", n)
				_, _ = r.Record(Batch{Now: n, Events: []Event{ev("api", true)}})
				_, _ = r.Sweep(n)
				_ = r.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	snap := r.Snapshot()
	if snap.Now != 49 {
		t.Fatalf("now=%d", snap.Now)
	}
}
