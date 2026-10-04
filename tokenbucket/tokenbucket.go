package tokenbucket

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("bucket not found")
	ErrTimeBackwards  = errors.New("time moved backwards")
	ErrInsufficient   = errors.New("insufficient tokens")
	ErrOverflow       = errors.New("capacity overflow")
)

type Kind uint8

const (
	Acquire Kind = iota + 1
	Refund
)

type Options struct{ MaxBuckets, MaxNameBytes int }
type BucketSpec struct {
	Name                                string
	Capacity, RefillTokens, RefillEvery int64
}
type Change struct {
	Kind       Kind
	Bucket     string
	Tokens, At int64
}
type BucketState struct {
	Name                                                   string
	Capacity, Tokens, LastRefill, LastObserved, NextRefill int64
}
type Snapshot struct {
	Generation uint64
	Buckets    []BucketState
}

type bucket struct {
	capacity, refillTokens, refillEvery int64
	tokens, lastRefill, lastObserved    int64
}

type Registry struct {
	mu      sync.Mutex
	maxName int
	buckets map[string]*bucket
	gen     uint64
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func New(o Options, specs []BucketSpec) (*Registry, error) {
	if o.MaxBuckets <= 0 || o.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if len(specs) == 0 || len(specs) > o.MaxBuckets {
		return nil, ErrInvalidOptions
	}
	r := &Registry{maxName: o.MaxNameBytes, buckets: make(map[string]*bucket, len(specs))}
	for _, s := range specs {
		if !validName(s.Name, o.MaxNameBytes) {
			return nil, ErrInvalidOptions
		}
		if s.Capacity <= 0 || s.RefillTokens <= 0 || s.RefillEvery <= 0 {
			return nil, ErrInvalidOptions
		}
		if _, dup := r.buckets[s.Name]; dup {
			return nil, ErrInvalidOptions
		}
		r.buckets[s.Name] = &bucket{
			capacity: s.Capacity, refillTokens: s.RefillTokens, refillEvery: s.RefillEvery,
			tokens: s.Capacity,
		}
	}
	return r, nil
}

// refill advances b to explicit time at, crediting whole refill periods.
// Caller guarantees at >= b.lastObserved. Returns true if any field changed.
func (b *bucket) refill(at int64) bool {
	changed := false
	elapsed := at - b.lastRefill
	periods := elapsed / b.refillEvery
	if periods > 0 {
		if b.tokens < b.capacity {
			// Saturate before multiplying to avoid overflow.
			if periods > (b.capacity-b.tokens)/b.refillTokens {
				b.tokens = b.capacity
			} else {
				b.tokens += periods * b.refillTokens
			}
		}
		b.lastRefill = at - elapsed%b.refillEvery
		changed = true
	}
	if at != b.lastObserved {
		b.lastObserved = at
		changed = true
	}
	return changed
}

func (b *bucket) state(name string) BucketState {
	s := BucketState{
		Name: name, Capacity: b.capacity, Tokens: b.tokens,
		LastRefill: b.lastRefill, LastObserved: b.lastObserved,
	}
	if b.tokens < b.capacity && b.refillEvery <= math.MaxInt64-b.lastRefill {
		s.NextRefill = b.lastRefill + b.refillEvery
	}
	return s
}

func (r *Registry) ApplyBatch(changes []Change) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(changes) == 0 {
		return r.gen, nil
	}
	for _, c := range changes {
		if c.Kind != Acquire && c.Kind != Refund {
			return 0, ErrInvalidInput
		}
		if !validName(c.Bucket, r.maxName) {
			return 0, ErrInvalidInput
		}
		if c.Tokens <= 0 || c.At < 0 {
			return 0, ErrInvalidInput
		}
	}
	// Isolated candidate: clone all bucket states.
	cand := make(map[string]*bucket, len(r.buckets))
	for name, b := range r.buckets {
		cp := *b
		cand[name] = &cp
	}
	for _, c := range changes {
		b, ok := cand[c.Bucket]
		if !ok {
			return 0, ErrNotFound
		}
		if c.At < b.lastObserved {
			return 0, ErrTimeBackwards
		}
		b.refill(c.At)
		switch c.Kind {
		case Acquire:
			if c.Tokens > b.tokens {
				return 0, ErrInsufficient
			}
			b.tokens -= c.Tokens
		case Refund:
			if c.Tokens > b.capacity-b.tokens {
				return 0, ErrOverflow
			}
			b.tokens += c.Tokens
		}
	}
	r.buckets = cand
	r.gen++
	return r.gen, nil
}

func (r *Registry) Inspect(name string, at int64) (BucketState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validName(name, r.maxName) || at < 0 {
		return BucketState{}, ErrInvalidInput
	}
	b, ok := r.buckets[name]
	if !ok {
		return BucketState{}, ErrNotFound
	}
	if at < b.lastObserved {
		return BucketState{}, ErrTimeBackwards
	}
	cp := *b
	cp.refill(at)
	return cp.state(name), nil
}

func (r *Registry) Sweep(at int64) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at < 0 {
		return 0, ErrInvalidInput
	}
	for _, b := range r.buckets {
		if at < b.lastObserved {
			return 0, ErrTimeBackwards
		}
	}
	changed := false
	for _, b := range r.buckets {
		if b.refill(at) {
			changed = true
		}
	}
	if changed {
		r.gen++
	}
	return r.gen, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.buckets))
	for name := range r.buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	s := Snapshot{Generation: r.gen, Buckets: make([]BucketState, 0, len(names))}
	for _, name := range names {
		s.Buckets = append(s.Buckets, r.buckets[name].state(name))
	}
	return s
}
