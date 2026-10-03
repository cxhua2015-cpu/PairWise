// Package join implements an event-time two-stream correlator.
package join

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalid        = errors.New("join: invalid input")
	ErrTime           = errors.New("join: watermark moved backwards")
	ErrLate           = errors.New("join: event is late")
	ErrConflict       = errors.New("join: event id conflict")
	ErrCapacity       = errors.New("join: capacity exceeded")
	ErrNotImplemented = errors.New("join: not implemented")
)

type Side uint8

const (
	Left Side = iota + 1
	Right
)

type Options struct {
	Window    int64
	MaxEvents int
	MaxBytes  int
}

type Event struct {
	Side    Side
	Key     string
	ID      string
	Time    int64
	Payload []byte
}

type Watermark struct {
	Side Side
	Time int64
}

// Update must contain exactly one of Event or Watermark.
type Update struct {
	Event     *Event
	Watermark *Watermark
}

type Match struct {
	Key       string
	LeftID    string
	RightID   string
	LeftTime  int64
	RightTime int64
	Left      []byte
	Right     []byte
}

type Expired struct {
	Side Side
	Key  string
	ID   string
	Time int64
}

type Outcome struct {
	Matches []Match
	Expired []Expired
}

type EventState struct {
	Side  Side
	Key   string
	ID    string
	Time  int64
	Bytes int
}

type Snapshot struct {
	LeftWatermark  int64
	RightWatermark int64
	Events         []EventState
	Count          int
	Bytes          int
}

type Joiner struct {
	mu        sync.Mutex
	window    int64
	maxEvents int
	maxBytes  int
	st        *state
}

const maxTime = 1_000_000_000_000

type storedEvent struct {
	side    Side
	key     string
	id      string
	time    int64
	payload []byte
}

func (e *storedEvent) bytes() int {
	return len(e.key) + len(e.id) + len(e.payload)
}

// sideState indexes the active events of one side by ID and by key.
type sideState struct {
	byID  map[string]*storedEvent
	byKey map[string]map[string]*storedEvent
}

func newSideState() *sideState {
	return &sideState{byID: map[string]*storedEvent{}, byKey: map[string]map[string]*storedEvent{}}
}

func (s *sideState) clone() *sideState {
	c := newSideState()
	for id, e := range s.byID {
		c.byID[id] = e
	}
	for k, ids := range s.byKey {
		m := make(map[string]*storedEvent, len(ids))
		for id, e := range ids {
			m[id] = e
		}
		c.byKey[k] = m
	}
	return c
}

func (s *sideState) add(e *storedEvent) {
	s.byID[e.id] = e
	ids := s.byKey[e.key]
	if ids == nil {
		ids = map[string]*storedEvent{}
		s.byKey[e.key] = ids
	}
	ids[e.id] = e
}

func (s *sideState) remove(e *storedEvent) {
	delete(s.byID, e.id)
	ids := s.byKey[e.key]
	delete(ids, e.id)
	if len(ids) == 0 {
		delete(s.byKey, e.key)
	}
}

// state is the mutable joiner state. Batches operate on a clone and commit
// atomically by swapping it in, so failures leave no side effects.
type state struct {
	sides [2]*sideState
	wm    [2]int64
	count int
	bytes int
}

func (st *state) clone() *state {
	return &state{
		sides: [2]*sideState{st.sides[0].clone(), st.sides[1].clone()},
		wm:    st.wm,
		count: st.count,
		bytes: st.bytes,
	}
}

func sideIndex(s Side) int { return int(s) - 1 }

func New(opts Options) (*Joiner, error) {
	if opts.Window <= 0 || opts.Window > maxTime || opts.MaxEvents <= 0 || opts.MaxBytes <= 0 {
		return nil, ErrInvalid
	}
	return &Joiner{
		window:    opts.Window,
		maxEvents: opts.MaxEvents,
		maxBytes:  opts.MaxBytes,
		st: &state{
			sides: [2]*sideState{newSideState(), newSideState()},
		},
	}, nil
}

func validSide(s Side) bool { return s == Left || s == Right }

func validateUpdate(u Update) error {
	if (u.Event == nil) == (u.Watermark == nil) {
		return ErrInvalid
	}
	if u.Event != nil {
		e := u.Event
		if !validSide(e.Side) || e.Key == "" || e.ID == "" || e.Time < 0 || e.Time > maxTime {
			return ErrInvalid
		}
		return nil
	}
	w := u.Watermark
	if !validSide(w.Side) || w.Time < 0 || w.Time > maxTime {
		return ErrInvalid
	}
	return nil
}

func (j *Joiner) Apply(update Update) (Outcome, error) {
	outs, err := j.ApplyBatch([]Update{update})
	if err != nil {
		return Outcome{}, err
	}
	return outs[0], nil
}

func (j *Joiner) ApplyBatch(updates []Update) ([]Outcome, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	work := j.st.clone()
	outs := make([]Outcome, 0, len(updates))
	for _, u := range updates {
		if err := validateUpdate(u); err != nil {
			return nil, err
		}
		var out Outcome
		var err error
		if u.Event != nil {
			out, err = j.applyEvent(work, u.Event)
		} else {
			out, err = j.applyWatermark(work, u.Watermark)
		}
		if err != nil {
			return nil, err
		}
		outs = append(outs, out)
	}
	if work.count > j.maxEvents || work.bytes > j.maxBytes {
		return nil, ErrCapacity
	}
	j.st = work
	return outs, nil
}

// applyEvent validates and applies one event against the working state.
// Conflict and idempotent-duplicate checks run before lateness checks.
func (j *Joiner) applyEvent(st *state, in *Event) (Outcome, error) {
	si := sideIndex(in.Side)
	side := st.sides[si]
	if prev, ok := side.byID[in.ID]; ok {
		if prev.key == in.Key && prev.time == in.Time && bytes.Equal(prev.payload, in.Payload) {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	oi := 1 - si
	// in.Time and j.window are both <= 1e12, so their sum cannot overflow.
	if in.Time < st.wm[si] || st.wm[oi] > in.Time+j.window {
		return Outcome{}, ErrLate
	}
	e := &storedEvent{
		side:    in.Side,
		key:     in.Key,
		id:      in.ID,
		time:    in.Time,
		payload: bytes.Clone(in.Payload),
	}
	other := st.sides[oi]
	var matches []Match
	for _, o := range other.byKey[in.Key] {
		diff := o.time - e.time
		if diff < 0 {
			diff = -diff
		}
		if diff > j.window {
			continue
		}
		m := Match{Key: e.key}
		if e.side == Left {
			m.LeftID, m.LeftTime, m.Left = e.id, e.time, bytes.Clone(e.payload)
			m.RightID, m.RightTime, m.Right = o.id, o.time, bytes.Clone(o.payload)
		} else {
			m.LeftID, m.LeftTime, m.Left = o.id, o.time, bytes.Clone(o.payload)
			m.RightID, m.RightTime, m.Right = e.id, e.time, bytes.Clone(e.payload)
		}
		matches = append(matches, m)
	}
	sort.Slice(matches, func(a, b int) bool {
		if e.side == Left {
			if matches[a].RightTime != matches[b].RightTime {
				return matches[a].RightTime < matches[b].RightTime
			}
			return matches[a].RightID < matches[b].RightID
		}
		if matches[a].LeftTime != matches[b].LeftTime {
			return matches[a].LeftTime < matches[b].LeftTime
		}
		return matches[a].LeftID < matches[b].LeftID
	})
	side.add(e)
	st.count++
	st.bytes += e.bytes()
	return Outcome{Matches: matches}, nil
}

// applyWatermark advances one side's watermark and expires opposite-side
// events whose time+window is strictly below the new watermark.
func (j *Joiner) applyWatermark(st *state, w *Watermark) (Outcome, error) {
	si := sideIndex(w.Side)
	if w.Time < st.wm[si] {
		return Outcome{}, ErrTime
	}
	if w.Time == st.wm[si] {
		return Outcome{}, nil
	}
	st.wm[si] = w.Time
	other := st.sides[1-si]
	var expired []Expired
	for _, e := range other.byID {
		// e.time and j.window are both <= 1e12, so the sum cannot overflow.
		if e.time+j.window < w.Time {
			expired = append(expired, Expired{Side: e.side, Key: e.key, ID: e.id, Time: e.time})
			other.remove(e)
			st.count--
			st.bytes -= e.bytes()
		}
	}
	sort.Slice(expired, func(a, b int) bool {
		if expired[a].Time != expired[b].Time {
			return expired[a].Time < expired[b].Time
		}
		if expired[a].Key != expired[b].Key {
			return expired[a].Key < expired[b].Key
		}
		return expired[a].ID < expired[b].ID
	})
	return Outcome{Expired: expired}, nil
}

func (j *Joiner) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	snap := Snapshot{
		LeftWatermark:  j.st.wm[0],
		RightWatermark: j.st.wm[1],
		Count:          j.st.count,
		Bytes:          j.st.bytes,
	}
	for _, side := range j.st.sides {
		for _, e := range side.byID {
			snap.Events = append(snap.Events, EventState{
				Side:  e.side,
				Key:   e.key,
				ID:    e.id,
				Time:  e.time,
				Bytes: e.bytes(),
			})
		}
	}
	sort.Slice(snap.Events, func(a, b int) bool {
		if snap.Events[a].Side != snap.Events[b].Side {
			return snap.Events[a].Side < snap.Events[b].Side
		}
		if snap.Events[a].Time != snap.Events[b].Time {
			return snap.Events[a].Time < snap.Events[b].Time
		}
		if snap.Events[a].Key != snap.Events[b].Key {
			return snap.Events[a].Key < snap.Events[b].Key
		}
		return snap.Events[a].ID < snap.Events[b].ID
	})
	return snap
}
