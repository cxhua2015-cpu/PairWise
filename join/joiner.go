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

const maxTime = 1_000_000_000_000

// rec is an immutable stored event.
type rec struct {
	key     string
	id      string
	time    int64
	payload []byte
	size    int // len(key)+len(id)+len(payload)
}

// state is the mutable transactional state. All recs are immutable and all
// index slices are copy-on-write, so a shallow clone is a valid checkpoint.
type state struct {
	wm    [2]int64
	byID  [2]map[string]*rec
	byKey [2]map[string][]*rec // per key, sorted by (time, id)
	count int
	bytes int
}

func newState() *state {
	s := &state{}
	for i := range s.byID {
		s.byID[i] = map[string]*rec{}
		s.byKey[i] = map[string][]*rec{}
	}
	return s
}

func (s *state) clone() *state {
	c := &state{wm: s.wm, count: s.count, bytes: s.bytes}
	for i := range s.byID {
		c.byID[i] = make(map[string]*rec, len(s.byID[i]))
		for k, v := range s.byID[i] {
			c.byID[i][k] = v
		}
		c.byKey[i] = make(map[string][]*rec, len(s.byKey[i]))
		for k, v := range s.byKey[i] {
			c.byKey[i][k] = v
		}
	}
	return c
}

func sideIdx(side Side) int { return int(side) - 1 }

// locate returns the index of r within list (sorted by time, id).
func locate(list []*rec, r *rec) int {
	return sort.Search(len(list), func(i int) bool {
		if list[i].time != r.time {
			return list[i].time > r.time
		}
		return list[i].id >= r.id
	})
}

func (s *state) insert(side Side, r *rec) {
	i := sideIdx(side)
	s.byID[i][r.id] = r
	list := s.byKey[i][r.key]
	at := locate(list, r)
	nl := make([]*rec, 0, len(list)+1)
	nl = append(nl, list[:at]...)
	nl = append(nl, r)
	nl = append(nl, list[at:]...)
	s.byKey[i][r.key] = nl
	s.count++
	s.bytes += r.size
}

func (s *state) remove(side Side, r *rec) {
	i := sideIdx(side)
	delete(s.byID[i], r.id)
	list := s.byKey[i][r.key]
	at := locate(list, r)
	nl := make([]*rec, 0, len(list)-1)
	nl = append(nl, list[:at]...)
	nl = append(nl, list[at+1:]...)
	if len(nl) == 0 {
		delete(s.byKey[i], r.key)
	} else {
		s.byKey[i][r.key] = nl
	}
	s.count--
	s.bytes -= r.size
}

type Joiner struct {
	mu     sync.Mutex
	window int64
	maxEv  int
	maxBy  int
	st     *state
}

func New(opts Options) (*Joiner, error) {
	if opts.Window <= 0 || opts.Window > maxTime || opts.MaxEvents <= 0 || opts.MaxBytes <= 0 {
		return nil, ErrInvalid
	}
	return &Joiner{window: opts.Window, maxEv: opts.MaxEvents, maxBy: opts.MaxBytes, st: newState()}, nil
}

func validSide(s Side) bool { return s == Left || s == Right }

func (j *Joiner) validate(u Update) error {
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

// applyEvent mutates st; caller guarantees transactional context.
func (j *Joiner) applyEvent(st *state, e *Event) (Outcome, error) {
	i := sideIdx(e.Side)
	if old, ok := st.byID[i][e.ID]; ok {
		if old.key == e.Key && old.time == e.Time && bytes.Equal(old.payload, e.Payload) {
			return Outcome{}, nil // idempotent duplicate
		}
		return Outcome{}, ErrConflict
	}
	if e.Time < st.wm[i] {
		return Outcome{}, ErrLate
	}
	if st.wm[1-i] > e.Time+j.window {
		return Outcome{}, ErrLate
	}
	r := &rec{key: e.Key, id: e.ID, time: e.Time, size: len(e.Key) + len(e.ID) + len(e.Payload)}
	if e.Payload != nil {
		r.payload = append([]byte(nil), e.Payload...)
	}
	var matches []Match
	for _, o := range st.byKey[1-i][e.Key] {
		d := o.time - e.Time
		if d < 0 {
			d = -d
		}
		if d > j.window {
			continue
		}
		m := Match{Key: e.Key}
		if e.Side == Left {
			m.LeftID, m.LeftTime, m.Left = r.id, r.time, append([]byte(nil), r.payload...)
			m.RightID, m.RightTime, m.Right = o.id, o.time, append([]byte(nil), o.payload...)
		} else {
			m.LeftID, m.LeftTime, m.Left = o.id, o.time, append([]byte(nil), o.payload...)
			m.RightID, m.RightTime, m.Right = r.id, r.time, append([]byte(nil), r.payload...)
		}
		matches = append(matches, m)
	}
	st.insert(e.Side, r)
	return Outcome{Matches: matches}, nil
}

func (j *Joiner) applyWatermark(st *state, w *Watermark) (Outcome, error) {
	i := sideIdx(w.Side)
	if w.Time < st.wm[i] {
		return Outcome{}, ErrTime
	}
	st.wm[i] = w.Time
	var expired []Expired
	opp := 1 - i
	var oppSide Side = Left
	if opp == 1 {
		oppSide = Right
	}
	for _, r := range st.byID[opp] {
		if r.time+j.window < w.Time {
			expired = append(expired, Expired{Side: oppSide, Key: r.key, ID: r.id, Time: r.time})
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
	for _, x := range expired {
		st.remove(oppSide, st.byID[opp][x.ID])
	}
	return Outcome{Expired: expired}, nil
}

func (j *Joiner) Apply(update Update) (Outcome, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	out, err := j.applyBatchLocked([]Update{update})
	if err != nil {
		return Outcome{}, err
	}
	return out[0], nil
}

func (j *Joiner) ApplyBatch(updates []Update) ([]Outcome, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.applyBatchLocked(updates)
}

func (j *Joiner) applyBatchLocked(updates []Update) ([]Outcome, error) {
	for _, u := range updates {
		if err := j.validate(u); err != nil {
			return nil, err
		}
	}
	st := j.st.clone()
	outcomes := make([]Outcome, 0, len(updates))
	for _, u := range updates {
		var o Outcome
		var err error
		if u.Event != nil {
			o, err = j.applyEvent(st, u.Event)
		} else {
			o, err = j.applyWatermark(st, u.Watermark)
		}
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, o)
	}
	if st.count > j.maxEv || st.bytes > j.maxBy {
		return nil, ErrCapacity
	}
	j.st = st
	return outcomes, nil
}

func (j *Joiner) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.st
	snap := Snapshot{
		LeftWatermark:  st.wm[0],
		RightWatermark: st.wm[1],
		Events:         make([]EventState, 0, st.count),
		Count:          st.count,
		Bytes:          st.bytes,
	}
	for i := 0; i < 2; i++ {
		side := Left
		if i == 1 {
			side = Right
		}
		for _, r := range st.byID[i] {
			snap.Events = append(snap.Events, EventState{Side: side, Key: r.key, ID: r.id, Time: r.time, Bytes: r.size})
		}
	}
	sort.Slice(snap.Events, func(a, b int) bool {
		x, y := snap.Events[a], snap.Events[b]
		if x.Side != y.Side {
			return x.Side < y.Side
		}
		if x.Time != y.Time {
			return x.Time < y.Time
		}
		if x.Key != y.Key {
			return x.Key < y.Key
		}
		return x.ID < y.ID
	})
	return snap
}
