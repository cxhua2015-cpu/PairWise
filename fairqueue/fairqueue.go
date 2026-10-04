package fairqueue

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("task exists")
	ErrNotFound       = errors.New("task not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxTasks, MaxPayloadBytes, MaxNameBytes int }
type QueueWeight struct {
	Queue  string
	Weight int
}
type Task struct {
	ID, Queue string
	Sequence  uint64
	Payload   []byte
}
type Change struct {
	Kind      Kind
	ID, Queue string
	Payload   []byte
}
type ScheduleResult struct {
	Generation uint64
	Cursor     int
	Tasks      []Task
}
type Snapshot struct {
	Generation          uint64
	NextSequence        uint64
	Cursor              int
	Tasks, PayloadBytes int
	Wheel               []string
	Items               []Task
}

type state struct {
	byID         map[string]*Task
	byQueue      map[string][]*Task
	nextSequence uint64
	payloadBytes int
}

type Queue struct {
	mu              sync.Mutex
	maxTasks        int
	maxPayloadBytes int
	nameBytes       int
	queues          map[string]bool
	wheel           []string
	cursor          int
	generation      uint64
	st              *state
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func New(o Options, ws []QueueWeight) (*Queue, error) {
	if o.MaxTasks <= 0 || o.MaxPayloadBytes <= 0 || o.MaxNameBytes <= 0 || len(ws) == 0 {
		return nil, ErrInvalidOptions
	}
	queues := make(map[string]bool, len(ws))
	sum := 0
	for _, w := range ws {
		if !validName(w.Queue, o.MaxNameBytes) || queues[w.Queue] || w.Weight < 1 || w.Weight > 64 {
			return nil, ErrInvalidOptions
		}
		queues[w.Queue] = true
		sum += w.Weight
	}
	if sum > 1024 {
		return nil, ErrInvalidOptions
	}
	names := make([]string, 0, len(ws))
	for _, w := range ws {
		names = append(names, w.Queue)
	}
	sort.Strings(names)
	weightOf := make(map[string]int, len(ws))
	for _, w := range ws {
		weightOf[w.Queue] = w.Weight
	}
	wheel := make([]string, 0, sum)
	for _, n := range names {
		for i := 0; i < weightOf[n]; i++ {
			wheel = append(wheel, n)
		}
	}
	byQueue := make(map[string][]*Task, len(names))
	for _, n := range names {
		byQueue[n] = nil
	}
	return &Queue{
		maxTasks:        o.MaxTasks,
		maxPayloadBytes: o.MaxPayloadBytes,
		nameBytes:       o.MaxNameBytes,
		queues:          queues,
		wheel:           wheel,
		st:              &state{byID: map[string]*Task{}, byQueue: byQueue, nextSequence: 1},
	}, nil
}

func cloneTask(t *Task) *Task {
	cp := *t
	cp.Payload = append([]byte(nil), t.Payload...)
	return &cp
}

func (s *state) clone() *state {
	out := &state{
		byID:         make(map[string]*Task, len(s.byID)),
		byQueue:      make(map[string][]*Task, len(s.byQueue)),
		nextSequence: s.nextSequence,
		payloadBytes: s.payloadBytes,
	}
	for name, fifo := range s.byQueue {
		nf := make([]*Task, len(fifo))
		for i, t := range fifo {
			ct := cloneTask(t)
			nf[i] = ct
			out.byID[ct.ID] = ct
		}
		out.byQueue[name] = nf
	}
	return out
}

func (q *Queue) validateChange(c Change) error {
	switch c.Kind {
	case Put:
		if !validName(c.ID, q.maxNameBytes()) || !q.queues[c.Queue] {
			return ErrInvalidInput
		}
		if c.Payload == nil || len(c.Payload) > q.maxPayloadBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(c.ID, q.maxNameBytes()) || c.Queue != "" || c.Payload != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// maxNameBytes is derived from configured queue names; IDs share the name rules.
func (q *Queue) maxNameBytes() int { return q.nameBytes }

func (q *Queue) ApplyBatch(changes []Change) (uint64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(changes) == 0 {
		return q.generation, nil
	}
	for _, c := range changes {
		if err := q.validateChange(c); err != nil {
			return 0, err
		}
	}
	cand := q.st.clone()
	for _, c := range changes {
		switch c.Kind {
		case Put:
			if _, ok := cand.byID[c.ID]; ok {
				return 0, ErrExists
			}
			t := &Task{ID: c.ID, Queue: c.Queue, Sequence: cand.nextSequence,
				Payload: append([]byte(nil), c.Payload...)}
			cand.nextSequence++
			cand.byID[t.ID] = t
			cand.byQueue[t.Queue] = append(cand.byQueue[t.Queue], t)
			cand.payloadBytes += len(t.Payload)
		case Delete:
			t, ok := cand.byID[c.ID]
			if !ok {
				return 0, ErrNotFound
			}
			delete(cand.byID, c.ID)
			fifo := cand.byQueue[t.Queue]
			for i, ft := range fifo {
				if ft.ID == c.ID {
					cand.byQueue[t.Queue] = append(fifo[:i], fifo[i+1:]...)
					break
				}
			}
			cand.payloadBytes -= len(t.Payload)
		}
	}
	if len(cand.byID) > q.maxTasks || cand.payloadBytes > q.maxPayloadBytes {
		return 0, ErrCapacity
	}
	q.st = cand
	q.generation++
	return q.generation, nil
}

// schedule simulates the wheel from cursor and returns selected tasks in
// scheduling order plus the resulting cursor. Oldest task per queue is
// selected; empty queues are skipped; a full wheel scan without selection
// stops the schedule.
func (q *Queue) schedule(st *state, cursor, limit int) ([]*Task, int) {
	var out []*Task
	for len(out) < limit {
		picked := false
		for i := 0; i < len(q.wheel); i++ {
			name := q.wheel[cursor]
			cursor = (cursor + 1) % len(q.wheel)
			if fifo := st.byQueue[name]; len(fifo) > 0 {
				out = append(out, fifo[0])
				st.byQueue[name] = fifo[1:]
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return out, cursor
}

func (q *Queue) checkLimit(limit int) error {
	if limit < 1 || limit > 1000 {
		return ErrInvalidInput
	}
	return nil
}

func (q *Queue) Peek(limit int) (ScheduleResult, error) {
	if err := q.checkLimit(limit); err != nil {
		return ScheduleResult{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	sim := q.st.clone()
	picked, cursor := q.schedule(sim, q.cursor, limit)
	res := ScheduleResult{Generation: q.generation, Cursor: cursor}
	for _, t := range picked {
		res.Tasks = append(res.Tasks, *cloneTask(t))
	}
	return res, nil
}

func (q *Queue) Dequeue(limit int) (ScheduleResult, error) {
	if err := q.checkLimit(limit); err != nil {
		return ScheduleResult{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	cand := q.st.clone()
	picked, cursor := q.schedule(cand, q.cursor, limit)
	res := ScheduleResult{Generation: q.generation, Cursor: cursor}
	if len(picked) == 0 {
		return res, nil
	}
	for _, t := range picked {
		delete(cand.byID, t.ID)
		cand.payloadBytes -= len(t.Payload)
		res.Tasks = append(res.Tasks, *cloneTask(t))
	}
	q.st = cand
	q.cursor = cursor
	q.generation++
	res.Generation = q.generation
	return res, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := Snapshot{
		Generation:   q.generation,
		NextSequence: q.st.nextSequence,
		Cursor:       q.cursor,
		Tasks:        len(q.st.byID),
		PayloadBytes: q.st.payloadBytes,
		Wheel:        append([]string(nil), q.wheel...),
	}
	for _, t := range q.st.byID {
		s.Items = append(s.Items, *cloneTask(t))
	}
	sort.Slice(s.Items, func(i, j int) bool {
		if s.Items[i].Queue != s.Items[j].Queue {
			return s.Items[i].Queue < s.Items[j].Queue
		}
		return s.Items[i].Sequence < s.Items[j].Sequence
	})
	return s
}
