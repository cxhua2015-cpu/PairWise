package deadlinequeue

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
	Add Kind = iota + 1
	Delete
)

type Options struct{ MaxTasks, MaxPayloadBytes, MaxNameBytes int }
type Task struct {
	ID, Queue string
	Due       int64
	Priority  int32
	Payload   []byte
}
type Change struct {
	Kind Kind
	Task Task
}
type Snapshot struct {
	Generation          uint64
	Tasks, PayloadBytes int
	Items               []Task
}

type Queue struct {
	mu           sync.Mutex
	maxTasks     int
	maxPayload   int
	maxName      int
	tasks        map[string]Task
	payloadBytes int
	generation   uint64
}

func New(opts Options) (*Queue, error) {
	if opts.MaxTasks <= 0 || opts.MaxPayloadBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxTasks:   opts.MaxTasks,
		maxPayload: opts.MaxPayloadBytes,
		maxName:    opts.MaxNameBytes,
		tasks:      make(map[string]Task),
	}, nil
}

func (q *Queue) validName(s string) bool {
	return len(s) > 0 && len(s) <= q.maxName
}

func (q *Queue) validTask(t Task) bool {
	return q.validName(t.ID) && q.validName(t.Queue) && t.Due >= 0 &&
		t.Payload != nil && len(t.Payload) <= q.maxPayload
}

func (q *Queue) validateChange(c Change) bool {
	switch c.Kind {
	case Add:
		return q.validTask(c.Task)
	case Delete:
		t := c.Task
		return q.validName(t.ID) && t.Queue == "" && t.Due == 0 && t.Priority == 0 && t.Payload == nil
	default:
		return false
	}
}

func clonePayload(p []byte) []byte {
	c := make([]byte, len(p))
	copy(c, p)
	return c
}

func cloneTask(t Task) Task {
	t.Payload = clonePayload(t.Payload)
	return t
}

func lessCanonical(a, b Task) bool {
	if a.Queue != b.Queue {
		return a.Queue < b.Queue
	}
	return lessWithinQueue(a, b)
}

func lessWithinQueue(a, b Task) bool {
	if a.Due != b.Due {
		return a.Due < b.Due
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.ID < b.ID
}

func (q *Queue) ApplyBatch(changes []Change) (uint64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(changes) == 0 {
		return q.generation, nil
	}
	for _, c := range changes {
		if !q.validateChange(c) {
			return 0, ErrInvalidInput
		}
	}
	candidate := make(map[string]Task, len(q.tasks)+len(changes))
	for id, t := range q.tasks {
		candidate[id] = t
	}
	payloadBytes := q.payloadBytes
	for _, c := range changes {
		switch c.Kind {
		case Add:
			t := cloneTask(c.Task)
			if _, ok := candidate[t.ID]; ok {
				return 0, ErrExists
			}
			candidate[t.ID] = t
			payloadBytes += len(t.Payload)
		case Delete:
			old, ok := candidate[c.Task.ID]
			if !ok {
				return 0, ErrNotFound
			}
			delete(candidate, c.Task.ID)
			payloadBytes -= len(old.Payload)
		}
	}
	if len(candidate) > q.maxTasks || payloadBytes > q.maxPayload {
		return 0, ErrCapacity
	}
	q.tasks = candidate
	q.payloadBytes = payloadBytes
	q.generation++
	return q.generation, nil
}

func (q *Queue) PopDue(queue string, now int64, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.validName(queue) || now < 0 || limit < 0 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	var due []Task
	for _, t := range q.tasks {
		if t.Queue == queue && t.Due <= now {
			due = append(due, t)
		}
	}
	sort.Slice(due, func(i, j int) bool { return lessWithinQueue(due[i], due[j]) })
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	if len(due) == 0 {
		return nil, nil
	}
	out := make([]Task, len(due))
	for i, t := range due {
		out[i] = cloneTask(t)
		delete(q.tasks, t.ID)
		q.payloadBytes -= len(t.Payload)
	}
	q.generation++
	return out, nil
}

func (q *Queue) Window(queue string, start, end int64, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.validName(queue) || start < 0 || start > end || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	var out []Task
	for _, t := range q.tasks {
		if t.Queue == queue && t.Due >= start && t.Due < end {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessWithinQueue(out[i], out[j]) })
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i] = cloneTask(out[i])
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := Snapshot{
		Generation:   q.generation,
		Tasks:        len(q.tasks),
		PayloadBytes: q.payloadBytes,
		Items:        make([]Task, 0, len(q.tasks)),
	}
	for _, t := range q.tasks {
		s.Items = append(s.Items, cloneTask(t))
	}
	sort.Slice(s.Items, func(i, j int) bool { return lessCanonical(s.Items[i], s.Items[j]) })
	return s
}
