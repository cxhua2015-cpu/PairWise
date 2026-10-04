// Package windowcounter implements a concurrency-safe, exact sliding-window
// counter registry driven by explicit time. See SPEC.md.
package windowcounter

import (
	"errors"
	"math"
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
	keys       map[string]*keyState
	totalEv    int
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

// expireInto removes events with at <= now-window from m, updating sums and
// the running event total. It returns the number of removed events.
func expireInto(m map[string]*keyState, window, now int64, total int) (int, int) {
	cutoff := now - window // cannot overflow: now >= 0, window > 0
	expired := 0
	for k, ks := range m {
		kept := ks.events[:0]
		var sum int64
		for _, ev := range ks.events {
			if ev.at > cutoff {
				kept = append(kept, ev)
				sum += ev.amount // exact: retained events previously summed without overflow
			} else {
				expired++
			}
		}
		if len(kept) == 0 {
			delete(m, k)
			total -= len(ks.events)
		} else {
			total -= len(ks.events) - len(kept)
			ks.events = kept
			ks.sum = sum
		}
	}
	return expired, total
}

func cloneState(src map[string]*keyState) map[string]*keyState {
	dst := make(map[string]*keyState, len(src))
	for k, ks := range src {
		ev := make([]event, len(ks.events))
		copy(ev, ks.events)
		dst[k] = &keyState{events: ev, sum: ks.sum}
	}
	return dst
}

func (r *Registry) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch, without reading state.
	for _, dlt := range b.Deltas {
		if !validKey(dlt.Key, r.maxName) || dlt.Amount == 0 {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if b.Now < r.now {
		return Result{}, ErrTime
	}

	cand := cloneState(r.keys)
	total := r.totalEv
	expired, total := expireInto(cand, r.window, b.Now, total)

	touched := make(map[string]struct{})
	for _, dlt := range b.Deltas {
		ks := cand[dlt.Key]
		if ks == nil {
			ks = &keyState{}
			cand[dlt.Key] = ks
		}
		if dlt.Amount > 0 && ks.sum > math.MaxInt64-dlt.Amount {
			return Result{}, ErrOverflow
		}
		sum := ks.sum + dlt.Amount // cannot int64-underflow: ks.sum >= 0
		if sum < 0 {
			return Result{}, ErrUnderflow
		}
		ks.sum = sum
		ks.events = append(ks.events, event{at: b.Now, amount: dlt.Amount})
		total++
		touched[dlt.Key] = struct{}{}
	}

	if len(cand) > r.maxKeys || total > r.maxEvents {
		return Result{}, ErrCapacity
	}

	r.keys = cand
	r.totalEv = total
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
	expired, total := expireInto(r.keys, r.window, now, r.totalEv)
	r.totalEv = total
	r.now = now
	if expired > 0 {
		r.generation++
	}
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
	expired, total := expireInto(r.keys, r.window, now, r.totalEv)
	r.totalEv = total
	r.now = now
	if expired > 0 {
		r.generation++
	}
	return expired, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		Generation: r.generation,
		Now:        r.now,
		Events:     r.totalEv,
		Keys:       make([]KeyState, 0, len(r.keys)),
	}
	for k, ks := range r.keys {
		s.Keys = append(s.Keys, KeyState{Key: k, Value: ks.sum, Events: len(ks.events)})
	}
	sort.Slice(s.Keys, func(i, j int) bool { return s.Keys[i].Key < s.Keys[j].Key })
	return s
}
