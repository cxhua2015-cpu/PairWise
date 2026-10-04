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

type Queue struct {
	mu         sync.Mutex
	maxTasks   int
	maxPayload int
	maxName    int
	wheel      []string
	queues     map[string][]Task // per-queue FIFO, ordered by Sequence
	ids        map[string]string // task ID -> queue name
	tasks      int
	payload    int
	nextSeq    uint64
	generation uint64
	cursor     int
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func New(o Options, ws []QueueWeight) (*Queue, error) {
	if o.MaxTasks <= 0 || o.MaxPayloadBytes <= 0 || o.MaxNameBytes <= 0 || len(ws) == 0 {
		return nil, ErrInvalidOptions
	}
	seen := make(map[string]bool, len(ws))
	sum := 0
	for _, w := range ws {
		if !validName(w.Queue, o.MaxNameBytes) || seen[w.Queue] || w.Weight < 1 || w.Weight > 64 {
			return nil, ErrInvalidOptions
		}
		seen[w.Queue] = true
		sum += w.Weight
		if sum > 1024 {
			return nil, ErrInvalidOptions
		}
	}
	sorted := make([]QueueWeight, len(ws))
	copy(sorted, ws)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Queue < sorted[j].Queue })
	wheel := make([]string, 0, sum)
	queues := make(map[string][]Task, len(sorted))
	for _, w := range sorted {
		queues[w.Queue] = nil
		for i := 0; i < w.Weight; i++ {
			wheel = append(wheel, w.Queue)
		}
	}
	return &Queue{
		maxTasks:   o.MaxTasks,
		maxPayload: o.MaxPayloadBytes,
		maxName:    o.MaxNameBytes,
		wheel:      wheel,
		queues:     queues,
		ids:        make(map[string]string),
		nextSeq:    1,
	}, nil
}

func (q *Queue) ApplyBatch(changes []Change) (uint64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(changes) == 0 {
		return q.generation, nil
	}
	// Structural validation of every change before any state lookup.
	for _, c := range changes {
		switch c.Kind {
		case Put:
			if !validName(c.ID, q.maxName) {
				return 0, ErrInvalidInput
			}
			if _, ok := q.queues[c.Queue]; !ok {
				return 0, ErrInvalidInput
			}
			if c.Payload == nil || len(c.Payload) > q.maxPayload {
				return 0, ErrInvalidInput
			}
		case Delete:
			if !validName(c.ID, q.maxName) || c.Queue != "" || c.Payload != nil {
				return 0, ErrInvalidInput
			}
		default:
			return 0, ErrInvalidInput
		}
	}
	// Execute on an isolated candidate.
	candQueues := make(map[string][]Task, len(q.queues))
	for name, fifo := range q.queues {
		cp := make([]Task, len(fifo))
		copy(cp, fifo)
		candQueues[name] = cp
	}
	candIDs := make(map[string]string, len(q.ids))
	for id, name := range q.ids {
		candIDs[id] = name
	}
	candTasks, candPayload, candSeq := q.tasks, q.payload, q.nextSeq
	for _, c := range changes {
		switch c.Kind {
		case Put:
			if _, ok := candIDs[c.ID]; ok {
				return 0, ErrExists
			}
			payload := make([]byte, len(c.Payload))
			copy(payload, c.Payload)
			candQueues[c.Queue] = append(candQueues[c.Queue], Task{ID: c.ID, Queue: c.Queue, Sequence: candSeq, Payload: payload})
			candIDs[c.ID] = c.Queue
			candSeq++
			candTasks++
			candPayload += len(payload)
		case Delete:
			name, ok := candIDs[c.ID]
			if !ok {
				return 0, ErrNotFound
			}
			fifo := candQueues[name]
			for i, t := range fifo {
				if t.ID == c.ID {
					candPayload -= len(t.Payload)
					candQueues[name] = append(fifo[:i], fifo[i+1:]...)
					break
				}
			}
			delete(candIDs, c.ID)
			candTasks--
		}
	}
	// Capacity is checked only against the final candidate state.
	if candTasks > q.maxTasks || candPayload > q.maxPayload {
		return 0, ErrCapacity
	}
	q.queues = candQueues
	q.ids = candIDs
	q.tasks = candTasks
	q.payload = candPayload
	q.nextSeq = candSeq
	q.generation++
	return q.generation, nil
}

// schedule simulates the weighted round-robin scan starting at cursor.
// It returns the selected tasks and the resulting cursor.
func (q *Queue) schedule(limit int, cursor int, queues map[string][]Task, remaining int) ([]Task, int) {
	if limit < 1 || limit > 1000 {
		return nil, cursor
	}
	selected := make([]Task, 0, limit)
	heads := make(map[string]int, len(queues))
	for len(selected) < limit && remaining > 0 {
		picked := false
		for i := 0; i < len(q.wheel); i++ {
			name := q.wheel[cursor]
			cursor = (cursor + 1) % len(q.wheel)
			fifo := queues[name]
			if heads[name] < len(fifo) {
				selected = append(selected, fifo[heads[name]])
				heads[name]++
				remaining--
				picked = true
				break
			}
		}
		if !picked {
			break
		}
	}
	return selected, cursor
}

func cloneTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i, t := range tasks {
		payload := make([]byte, len(t.Payload))
		copy(payload, t.Payload)
		t.Payload = payload
		out[i] = t
	}
	return out
}

func (q *Queue) Peek(limit int) (ScheduleResult, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit < 1 || limit > 1000 {
		return ScheduleResult{}, ErrInvalidInput
	}
	tasks, cursor := q.schedule(limit, q.cursor, q.queues, q.tasks)
	return ScheduleResult{Generation: q.generation, Cursor: cursor, Tasks: cloneTasks(tasks)}, nil
}

func (q *Queue) Dequeue(limit int) (ScheduleResult, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit < 1 || limit > 1000 {
		return ScheduleResult{}, ErrInvalidInput
	}
	tasks, cursor := q.schedule(limit, q.cursor, q.queues, q.tasks)
	if len(tasks) == 0 {
		return ScheduleResult{Generation: q.generation, Cursor: q.cursor, Tasks: nil}, nil
	}
	for _, t := range tasks {
		fifo := q.queues[t.Queue]
		q.queues[t.Queue] = fifo[1:]
		delete(q.ids, t.ID)
		q.tasks--
		q.payload -= len(t.Payload)
	}
	q.cursor = cursor
	q.generation++
	return ScheduleResult{Generation: q.generation, Cursor: cursor, Tasks: cloneTasks(tasks)}, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	names := make([]string, 0, len(q.queues))
	for name := range q.queues {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]Task, 0, q.tasks)
	for _, name := range names {
		items = append(items, q.queues[name]...)
	}
	wheel := make([]string, len(q.wheel))
	copy(wheel, q.wheel)
	return Snapshot{
		Generation:   q.generation,
		NextSequence: q.nextSeq,
		Cursor:       q.cursor,
		Tasks:        q.tasks,
		PayloadBytes: q.payload,
		Wheel:        wheel,
		Items:        cloneTasks(items),
	}
}
