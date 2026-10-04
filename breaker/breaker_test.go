package breaker

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
)

func opts() Options { return Options{MaxServices: 4, MaxNameBytes: 16} }

func pols() []Policy {
	return []Policy{
		{Name: "api", FailureThreshold: 2, RecoveryThreshold: 2, OpenFor: 5},
		{Name: "db", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 3},
	}
}

func newReg(t *testing.T) *Registry {
	t.Helper()
	r, err := New(opts(), pols())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		pols []Policy
	}{
		{"zero max services", Options{MaxServices: 0, MaxNameBytes: 4}, pols()},
		{"zero max name", Options{MaxServices: 4, MaxNameBytes: 0}, pols()},
		{"no policies", opts(), nil},
		{"too many", Options{MaxServices: 1, MaxNameBytes: 16}, pols()},
		{"empty name", opts(), []Policy{{Name: "", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"long name", opts(), []Policy{{Name: "abcdefghijklmnopq", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"bad char", opts(), []Policy{{Name: "a b", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}},
		{"zero failure threshold", opts(), []Policy{{Name: "x", FailureThreshold: 0, RecoveryThreshold: 1, OpenFor: 1}}},
		{"zero recovery threshold", opts(), []Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 0, OpenFor: 1}}},
		{"zero open for", opts(), []Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 0}}},
		{"negative open for", opts(), []Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: -1}}},
		{"dup names", opts(), []Policy{
			{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1},
			{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1},
		}},
	}
	for _, tc := range cases {
		if _, err := New(tc.opts, tc.pols); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: got %v", tc.name, err)
		}
	}
	// boundary: exactly MaxServices and MaxNameBytes is fine
	if _, err := New(Options{MaxServices: 1, MaxNameBytes: 1},
		[]Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordEmptyBatchGeneration(t *testing.T) {
	r := newReg(t)
	res, err := r.Record(Batch{Now: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generation != 0 || len(res.Changed) != 0 {
		t.Fatalf("res=%+v", res)
	}
	// open db, then empty batch past OpenUntil triggers transition + generation
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "db"}}}); err != nil {
		t.Fatal(err)
	}
	res, err = r.Record(Batch{Now: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generation != 2 || !reflect.DeepEqual(res.Changed, []string{"db"}) {
		t.Fatalf("res=%+v", res)
	}
}

func TestRollbackRestoresTimeAndCounters(t *testing.T) {
	r := newReg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "db"}}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	// api failure would count, then unknown service aborts the batch
	_, err := r.Record(Batch{Now: 2, Events: []Event{{Service: "api"}, {Service: "ghost"}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	if got := r.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("before=%+v got=%+v", before, got)
	}
	// time was rolled back, so now=1 is still acceptable (not backwards)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "api", Success: true}}}); err != nil {
		t.Fatal(err)
	}
}

func TestTimeMonotonicity(t *testing.T) {
	r := newReg(t)
	if _, err := r.Record(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(Batch{Now: 4}); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, _, err := r.Allow("api", 4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	if _, err := r.Sweep(4); !errors.Is(err, ErrTime) {
		t.Fatal(err)
	}
	// equal time is allowed
	if _, err := r.Record(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidInputCheckedBeforeTime(t *testing.T) {
	r := newReg(t)
	if _, err := r.Record(Batch{Now: 5}); err != nil {
		t.Fatal(err)
	}
	// invalid name + backwards time: structural validation wins
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "bad?"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, _, err := r.Allow("bad?", 1); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestMultipleTransitionsSameBatch(t *testing.T) {
	r := newReg(t)
	// api: open at now=1 (OpenUntil=6)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "api"}, {Service: "api"}}}); err != nil {
		t.Fatal(err)
	}
	// at now=6: auto HalfOpen, fail -> Open(11), then Sweep to 11 -> HalfOpen,
	// then two successes close it; all within explicit times
	res, err := r.Record(Batch{Now: 6, Events: []Event{{Service: "api"}}})
	if err != nil || !reflect.DeepEqual(res.Changed, []string{"api"}) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	s := r.Snapshot().Services[0]
	if s.Mode != Open || s.OpenUntil != 11 {
		t.Fatalf("s=%+v", s)
	}
	res, err = r.Record(Batch{Now: 11, Events: []Event{
		{Service: "api", Success: true},
		{Service: "api", Success: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Changed, []string{"api"}) {
		t.Fatalf("res=%+v", res)
	}
	if got := r.Snapshot().Services[0].Mode; got != Closed {
		t.Fatalf("mode=%v", got)
	}
}

func TestClosedSuccessResetsFailures(t *testing.T) {
	r := newReg(t)
	_, err := r.Record(Batch{Now: 1, Events: []Event{
		{Service: "api"}, {Service: "api", Success: true}, {Service: "api"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot().Services[0]
	if s.Mode != Closed || s.Failures != 1 {
		t.Fatalf("s=%+v", s)
	}
}

func TestSaturationAndExtremeTime(t *testing.T) {
	r, err := New(opts(), []Policy{{Name: "x", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: math.MaxInt64}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().Services[0].OpenUntil; got != math.MaxInt64 {
		t.Fatalf("openUntil=%d", got)
	}
	// even at MaxInt64 the breaker is due (now >= OpenUntil)
	changed, err := r.Sweep(math.MaxInt64)
	if err != nil || !reflect.DeepEqual(changed, []string{"x"}) {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestAllowSemantics(t *testing.T) {
	r := newReg(t)
	ok, st, err := r.Allow("api", 0)
	if err != nil || !ok || st.Mode != Closed {
		t.Fatalf("ok=%v st=%+v err=%v", ok, st, err)
	}
	if _, _, err := r.Allow("ghost", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "db"}}}); err != nil {
		t.Fatal(err)
	}
	ok, st, _ = r.Allow("db", 3)
	if ok || st.Mode != Open {
		t.Fatalf("ok=%v st=%+v", ok, st)
	}
	ok, st, _ = r.Allow("db", 4)
	if !ok || st.Mode != HalfOpen {
		t.Fatalf("ok=%v st=%+v", ok, st)
	}
	// Allow does not consume recovery successes
	if got := r.Snapshot().Services[1].RecoverySuccesses; got != 0 {
		t.Fatalf("recovery=%d", got)
	}
}

func TestSweepIsolationAndSorting(t *testing.T) {
	r, err := New(Options{MaxServices: 3, MaxNameBytes: 16}, []Policy{
		{Name: "svc-c", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 2},
		{Name: "svc-a", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 2},
		{Name: "svc-b", FailureThreshold: 1, RecoveryThreshold: 1, OpenFor: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(Batch{Now: 1, Events: []Event{
		{Service: "svc-c"}, {Service: "svc-a"}, {Service: "svc-b"},
	}}); err != nil {
		t.Fatal(err)
	}
	changed, err := r.Sweep(3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"svc-a", "svc-c"}) {
		t.Fatalf("changed=%v", changed)
	}
	// svc-b untouched
	snap := r.Snapshot()
	if snap.Services[1].Mode != Open {
		t.Fatalf("svc-b=%+v", snap.Services[1])
	}
	// snapshot sorted
	names := []string{snap.Services[0].Name, snap.Services[1].Name, snap.Services[2].Name}
	if !reflect.DeepEqual(names, []string{"svc-a", "svc-b", "svc-c"}) {
		t.Fatalf("names=%v", names)
	}
	// second sweep at same time: nothing changes, generation stable
	gen := snap.Generation
	changed, err = r.Sweep(3)
	if err != nil || len(changed) != 0 || r.Snapshot().Generation != gen {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestCrossServiceIsolation(t *testing.T) {
	r := newReg(t)
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "db"}}}); err != nil {
		t.Fatal(err)
	}
	// db is Open; api must be unaffected
	ok, st, err := r.Allow("api", 1)
	if err != nil || !ok || st.Mode != Closed {
		t.Fatalf("ok=%v st=%+v err=%v", ok, st, err)
	}
	if _, err := r.Record(Batch{Now: 1, Events: []Event{{Service: "api", Success: true}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotStableCopy(t *testing.T) {
	r := newReg(t)
	s1 := r.Snapshot()
	s1.Services[0].Mode = Open
	s1.Services[0].Failures = 99
	s2 := r.Snapshot()
	if s2.Services[0].Mode != Closed || s2.Services[0].Failures != 0 {
		t.Fatalf("snapshot mutated: %+v", s2.Services[0])
	}
}

func TestConcurrentMixed(t *testing.T) {
	r := newReg(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				now := int64(j)
				_, _ = r.Record(Batch{Now: now, Events: []Event{{Service: "api", Success: true}}})
				_, _, _ = r.Allow("db", now)
				_, _ = r.Sweep(now)
				_ = r.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	snap := r.Snapshot()
	if snap.Now != 49 {
		t.Fatalf("now=%d", snap.Now)
	}
	if snap.Services[0].Mode != Closed {
		t.Fatalf("mode=%v", snap.Services[0].Mode)
	}
}
