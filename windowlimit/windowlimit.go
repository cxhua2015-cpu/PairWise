package windowlimit

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	Window                                int64
	Limit                                 uint64
	MaxKeys, MaxKeyBytes, MaxEventsPerKey int
}
type Request struct {
	Key   string
	Units uint64
}
type Batch struct {
	Now      int64
	Requests []Request
}
type Decision struct {
	Key             string
	Allowed         bool
	Used, Remaining uint64
	Revision        uint64
}
type Event struct {
	At              int64
	Units, Revision uint64
}
type KeyState struct {
	Key    string
	Events []Event
}
type Result struct {
	Generation, Revision uint64
	Decisions            []Decision
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Keys                     []KeyState
}

type Limiter struct {
	mu         sync.Mutex
	window     int64
	limit      uint64
	maxKeys    int
	maxKeyByte int
	maxEvents  int
	now        int64
	generation uint64
	revision   uint64
	keys       map[string][]Event
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func New(o Options) (*Limiter, error) {
	if o.Window <= 0 || o.Limit == 0 || o.MaxKeys <= 0 || o.MaxKeyBytes <= 0 || o.MaxEventsPerKey <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Limiter{
		window:     o.Window,
		limit:      o.Limit,
		maxKeys:    o.MaxKeys,
		maxKeyByte: o.MaxKeyBytes,
		maxEvents:  o.MaxEventsPerKey,
		keys:       make(map[string][]Event),
	}, nil
}

func (l *Limiter) Check(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, r := range b.Requests {
		if !validKey(r.Key, l.maxKeyByte) || r.Units == 0 || r.Units > l.limit {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if b.Now < l.now {
		return Result{}, ErrTime
	}

	// Work on an isolated candidate so any failure rolls back cleanly.
	cutoff := b.Now - l.window
	cand := make(map[string][]Event, len(l.keys))
	pruned := false
	for k, evs := range l.keys {
		kept := evs[:0:0]
		for _, e := range evs {
			if e.At > cutoff {
				kept = append(kept, e)
			} else {
				pruned = true
			}
		}
		if len(kept) > 0 {
			cand[k] = kept
		}
	}

	sums := make(map[string]uint64, len(cand))
	for k, evs := range cand {
		var s uint64
		for _, e := range evs {
			s += e.Units
		}
		sums[k] = s
	}

	revision := l.revision
	allowedAny := false
	decisions := make([]Decision, len(b.Requests))
	for i, r := range b.Requests {
		used := sums[r.Key]
		d := Decision{Key: r.Key, Used: used, Remaining: l.limit - used}
		if used+r.Units <= l.limit {
			revision++
			cand[r.Key] = append(cand[r.Key], Event{At: b.Now, Units: r.Units, Revision: revision})
			sums[r.Key] = used + r.Units
			d.Allowed = true
			d.Used = used + r.Units
			d.Remaining = l.limit - d.Used
			d.Revision = revision
			allowedAny = true
		}
		decisions[i] = d
	}

	// Capacity is enforced only after all requests execute.
	if len(cand) > l.maxKeys {
		return Result{}, ErrCapacity
	}
	for _, evs := range cand {
		if len(evs) > l.maxEvents {
			return Result{}, ErrCapacity
		}
	}

	// Commit.
	generation := l.generation
	if pruned || allowedAny {
		generation++
	}
	l.now = b.Now
	l.generation = generation
	l.revision = revision
	l.keys = cand

	return Result{Generation: generation, Revision: revision, Decisions: decisions}, nil
}

func (l *Limiter) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	names := make([]string, 0, len(l.keys))
	for k := range l.keys {
		names = append(names, k)
	}
	sort.Strings(names)

	snap := Snapshot{
		Generation:   l.generation,
		NextRevision: l.revision + 1,
		Now:          l.now,
		Keys:         make([]KeyState, 0, len(names)),
	}
	for _, k := range names {
		evs := append([]Event(nil), l.keys[k]...)
		sort.SliceStable(evs, func(i, j int) bool {
			if evs[i].At != evs[j].At {
				return evs[i].At < evs[j].At
			}
			return evs[i].Revision < evs[j].Revision
		})
		snap.Keys = append(snap.Keys, KeyState{Key: k, Events: evs})
	}
	return snap
}
