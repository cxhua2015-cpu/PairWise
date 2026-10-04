// Package windowcounter implements a concurrency-safe, exact sliding-window
// counter registry driven by explicit time. See SPEC.md for the contract.
package windowcounter

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrUnderflow      = errors.New("counter underflow")
	ErrOverflow       = errors.New("counter overflow")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	Window                           int64
	MaxKeys, MaxEvents, MaxNameBytes int
}

type Delta struct {
	Key    string
	Amount int64
}

type Batch struct {
	Now    int64
	Deltas []Delta
}

type Count struct {
	Key   string
	Value int64
}

type Result struct {
	Generation    uint64
	ExpiredEvents int
	Counts        []Count
}

type KeyState struct {
	Key    string
	Value  int64
	Events int
}

type Snapshot struct {
	Generation uint64
	Now        int64
	Events     int
	Keys       []KeyState
}

type event struct {
	at     int64
	amount int64
}

type keyState struct {
	events []event
	sum    int64
}

type Registry struct {
	mu         sync.Mutex
	window     int64
	maxKeys    int
	maxEvents  int
	maxName    int
	now        int64
	generation uint64
	events     int
	keys       map[string]*keyState
}

func New(o Options) (*Registry, error) {
	if o.Window <= 0 || o.MaxKeys <= 0 || o.MaxEvents <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		window:    o.Window,
		maxKeys:   o.MaxKeys,
		maxEvents: o.MaxEvents,
		maxName:   o.MaxNameBytes,
		keys:      make(map[string]*keyState),
	}, nil
}

func validKey(k string, maxName int) bool {
	if len(k) == 0 || len(k) > maxName {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

// cutoff returns the exclusive retention bound: events with at <= cutoff expire.
// now and window are nonnegative, so now-window cannot overflow int64.
func (r *Registry) cutoff(now int64) int64 { return now - r.window }

// expireLocked removes all events at or before the cutoff for now, mutating st.
// Returns the number of removed events. Callers pass a cloned state when
// rollback isolation is required.
func expireState(st map[string]*keyState, cutoff int64, total *int) int {
	removed := 0
	for k, ks := range st {
		keep := ks.events[:0]
		var sum int64
		for _, ev := range ks.events {
			if ev.at > cutoff {
				keep = append(keep, ev)
				sum += ev.amount
			}
		}
		n := len(ks.events) - len(keep)
		if n == 0 {
			continue
		}
		removed += n
		*total -= n
		if len(keep) == 0 {
			delete(st, k)
			continue
		}
		ks.events = keep
		ks.sum = sum
	}
	return removed
}

func cloneState(src map[string]*keyState) map[string]*keyState {
	dst := make(map[string]*keyState, len(src))
	for k, ks := range src {
		evs := make([]event, len(ks.events))
		copy(evs, ks.events)
		dst[k] = &keyState{events: evs, sum: ks.sum}
	}
	return dst
}

func (r *Registry) Apply(b Batch) (Result, error) {
	// Phase 1: structural validation, no state reads.
	for _, dl := range b.Deltas {
		if !validKey(dl.Key, r.maxName) || dl.Amount == 0 {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Phase 2: time monotonicity.
	if b.Now < r.now {
		return Result{}, ErrTime
	}

	// Phase 3: work on an isolated candidate so any failure rolls back.
	cand := cloneState(r.keys)
	total := r.events
	expired := expireState(cand, r.cutoff(b.Now), &total)

	touched := make(map[string]struct{}, len(b.Deltas))
	for _, dl := range b.Deltas {
		ks := cand[dl.Key]
		if ks == nil {
			ks = &keyState{}
			cand[dl.Key] = ks
		}
		if dl.Amount > 0 && ks.sum > (1<<63-1)-dl.Amount {
			return Result{}, ErrOverflow
		}
		sum := ks.sum + dl.Amount
		if sum < 0 {
			return Result{}, ErrUnderflow
		}
		ks.sum = sum
		ks.events = append(ks.events, event{at: b.Now, amount: dl.Amount})
		total++
		touched[dl.Key] = struct{}{}
	}

	// Phase 4: final capacity checks only after all deltas.
	if len(cand) > r.maxKeys || total > r.maxEvents {
		return Result{}, ErrCapacity
	}

	// Commit.
	r.keys = cand
	r.events = total
	r.now = b.Now
	if expired > 0 || len(b.Deltas) > 0 {
		r.generation++
	}

	counts := make([]Count, 0, len(touched))
	for k := range touched {
		counts = append(counts, Count{Key: k, Value: cand[k].sum})
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i].Key < counts[j].Key })

	return Result{Generation: r.generation, ExpiredEvents: expired, Counts: counts}, nil
}

func (r *Registry) Get(key string, now int64) (int64, bool, error) {
	if !validKey(key, r.maxName) {
		return 0, false, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return 0, false, ErrTime
	}
	removed := expireState(r.keys, r.cutoff(now), &r.events)
	if removed > 0 {
		r.generation++
	}
	r.now = now
	ks := r.keys[key]
	if ks == nil {
		return 0, false, nil
	}
	return ks.sum, true, nil
}

func (r *Registry) Sweep(now int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return 0, ErrTime
	}
	removed := expireState(r.keys, r.cutoff(now), &r.events)
	if removed > 0 {
		r.generation++
	}
	r.now = now
	return removed, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		Generation: r.generation,
		Now:        r.now,
		Events:     r.events,
		Keys:       make([]KeyState, 0, len(r.keys)),
	}
	for k, ks := range r.keys {
		s.Keys = append(s.Keys, KeyState{Key: k, Value: ks.sum, Events: len(ks.events)})
	}
	sort.Slice(s.Keys, func(i, j int) bool { return s.Keys[i].Key < s.Keys[j].Key })
	return s
}
