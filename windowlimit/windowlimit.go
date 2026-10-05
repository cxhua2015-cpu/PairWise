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
	mu           sync.Mutex
	window       int64
	limit        uint64
	maxKeys      int
	maxKeyBytes  int
	maxEvents    int
	now          int64
	generation   uint64
	nextRevision uint64
	keys         map[string][]Event
}

func New(o Options) (*Limiter, error) {
	if o.Window <= 0 || o.Limit == 0 || o.MaxKeys <= 0 || o.MaxKeyBytes <= 0 || o.MaxEventsPerKey <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Limiter{
		window:      o.Window,
		limit:       o.Limit,
		maxKeys:     o.MaxKeys,
		maxKeyBytes: o.MaxKeyBytes,
		maxEvents:   o.MaxEventsPerKey,
		keys:        make(map[string][]Event),
	}, nil
}

func validKey(k string, maxBytes int) bool {
	if k == "" || len(k) > maxBytes {
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

func (l *Limiter) Check(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, r := range b.Requests {
		if !validKey(r.Key, l.maxKeyBytes) || r.Units == 0 || r.Units > l.limit {
			return Result{}, ErrInvalidInput
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if b.Now < l.now {
		return Result{}, ErrTime
	}

	// Isolated candidate: deep copy of state.
	cand := make(map[string][]Event, len(l.keys))
	for k, evs := range l.keys {
		cp := make([]Event, len(evs))
		copy(cp, evs)
		cand[k] = cp
	}
	now, generation, nextRevision := b.Now, l.generation, l.nextRevision

	// Prune events at or before Now-Window (left boundary excluded).
	cutoff := now - l.window
	pruned := false
	for k, evs := range cand {
		i := 0
		for i < len(evs) && evs[i].At <= cutoff {
			i++
		}
		if i > 0 {
			pruned = true
			rest := make([]Event, len(evs)-i)
			copy(rest, evs[i:])
			if len(rest) == 0 {
				delete(cand, k)
			} else {
				cand[k] = rest
			}
		}
	}

	allowedAny := false
	decisions := make([]Decision, len(b.Requests))
	used := make(map[string]uint64, len(cand))
	for i, r := range b.Requests {
		u, ok := used[r.Key]
		if !ok {
			for _, e := range cand[r.Key] {
				u += e.Units
			}
		}
		d := Decision{Key: r.Key}
		if u+r.Units <= l.limit {
			nextRevision++
			d.Allowed = true
			d.Revision = nextRevision
			u += r.Units
			cand[r.Key] = append(cand[r.Key], Event{At: now, Units: r.Units, Revision: nextRevision})
			allowedAny = true
		}
		d.Used = u
		d.Remaining = l.limit - u
		used[r.Key] = u
		decisions[i] = d
	}

	// Capacity checks on final state.
	if len(cand) > l.maxKeys {
		return Result{}, ErrCapacity
	}
	for _, evs := range cand {
		if len(evs) > l.maxEvents {
			return Result{}, ErrCapacity
		}
	}

	// Commit.
	if pruned || allowedAny {
		generation++
	}
	l.now = now
	l.generation = generation
	l.nextRevision = nextRevision
	l.keys = cand

	res := Result{Generation: generation, Revision: nextRevision, Decisions: decisions}
	return res, nil
}

func (l *Limiter) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := Snapshot{Generation: l.generation, NextRevision: l.nextRevision, Now: l.now}
	names := make([]string, 0, len(l.keys))
	for k := range l.keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		evs := make([]Event, len(l.keys[k]))
		copy(evs, l.keys[k])
		s.Keys = append(s.Keys, KeyState{Key: k, Events: evs})
	}
	return s
}
