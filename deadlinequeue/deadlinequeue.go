// Package deadlinequeue provides a concurrency-safe, in-memory, multi-queue
// deadline task table. See SPEC.md for the full contract.
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

// Queue is a concurrency-safe in-memory deadline task table.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	tasks        map[string]Task // by ID; Payload slices are privately owned
	payloadBytes int
	generation   uint64
}

func New(opts Options) (*Queue, error) {
	if opts.MaxTasks <= 0 || opts.MaxPayloadBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, tasks: make(map[string]Task)}, nil
}

func (q *Queue) validName(s string) bool {
	return len(s) > 0 && len(s) <= q.opts.MaxNameBytes
}

func (q *Queue) validateAdd(t *Task) error {
	if !q.validName(t.ID) || !q.validName(t.Queue) || t.Due < 0 ||
		t.Payload == nil || len(t.Payload) > q.opts.MaxPayloadBytes {
		return ErrInvalidInput
	}
	return nil
}

func (q *Queue) validateDelete(t *Task) error {
	if !q.validName(t.ID) || t.Queue != "" || t.Due != 0 || t.Priority != 0 || t.Payload != nil {
		return ErrInvalidInput
	}
	return nil
}

// ApplyBatch applies changes sequentially as one atomic transaction.
func (q *Queue) ApplyBatch(changes []Change) (uint64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(changes) == 0 {
		return q.generation, nil
	}

	// Phase 1: structural validation of every change, before any state lookup.
	for i := range changes {
		c := &changes[i]
		var err error
		switch c.Kind {
		case Add:
			err = q.validateAdd(&c.Task)
		case Delete:
			err = q.validateDelete(&c.Task)
		default:
			err = ErrInvalidInput
		}
		if err != nil {
			return q.generation, err
		}
	}

	// Phase 2: execute on an isolated candidate in input order.
	cand := make(map[string]Task, len(q.tasks))
	for id, t := range q.tasks {
		cand[id] = t
	}
	payloadBytes := q.payloadBytes
	for _, c := range changes {
		switch c.Kind {
		case Add:
			if _, ok := cand[c.Task.ID]; ok {
				return q.generation, ErrExists
			}
			t := c.Task
			t.Payload = append([]byte(nil), t.Payload...)
			cand[t.ID] = t
			payloadBytes += len(t.Payload)
		case Delete:
			t, ok := cand[c.Task.ID]
			if !ok {
				return q.generation, ErrNotFound
			}
			delete(cand, c.Task.ID)
			payloadBytes -= len(t.Payload)
		}
	}

	// Phase 3: capacity check on the final candidate only.
	if len(cand) > q.opts.MaxTasks || payloadBytes > q.opts.MaxPayloadBytes {
		return q.generation, ErrCapacity
	}

	q.tasks = cand
	q.payloadBytes = payloadBytes
	q.generation++
	return q.generation, nil
}

// lessQueue orders tasks within one queue: Due asc, Priority desc, ID asc.
func lessQueue(a, b Task) bool {
	if a.Due != b.Due {
		return a.Due < b.Due
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.ID < b.ID
}

// lessGlobal is the canonical order: queue asc, then within-queue order.
func lessGlobal(a, b Task) bool {
	if a.Queue != b.Queue {
		return a.Queue < b.Queue
	}
	return lessQueue(a, b)
}

func (q *Queue) queueTasksLocked(queue string) []Task {
	out := make([]Task, 0)
	for _, t := range q.tasks {
		if t.Queue == queue {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessQueue(out[i], out[j]) })
	return out
}

// PopDue removes up to limit tasks of queue with Due <= now, in stable order.
func (q *Queue) PopDue(queue string, now int64, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.validName(queue) || now < 0 || limit < 0 || limit > 1000 {
		return nil, ErrInvalidInput
	}

	var due []Task
	for _, t := range q.queueTasksLocked(queue) {
		if t.Due > now {
			break
		}
		if limit > 0 && len(due) == limit {
			break
		}
		due = append(due, t)
	}
	if len(due) == 0 {
		return nil, nil
	}
	for _, t := range due {
		delete(q.tasks, t.ID)
		q.payloadBytes -= len(t.Payload)
	}
	q.generation++
	return due, nil
}

// Window returns tasks of queue with start <= Due < end, without mutation.
func (q *Queue) Window(queue string, start, end int64, limit int) ([]Task, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.validName(queue) || start < 0 || start > end || limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}

	var out []Task
	for _, t := range q.queueTasksLocked(queue) {
		if t.Due >= end {
			break
		}
		if t.Due >= start {
			cp := t
			cp.Payload = append([]byte(nil), t.Payload...)
			out = append(out, cp)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// Snapshot reports generation, counts, and all tasks in canonical order.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Task, 0, len(q.tasks))
	for _, t := range q.tasks {
		cp := t
		cp.Payload = append([]byte(nil), t.Payload...)
		items = append(items, cp)
	}
	sort.Slice(items, func(i, j int) bool { return lessGlobal(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		Tasks:        len(q.tasks),
		PayloadBytes: q.payloadBytes,
		Items:        items,
	}
}
