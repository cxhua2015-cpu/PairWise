package leasegraph

import (
	"slices"
	"sort"
	"sync"
)

const (
	maxTasksLimit   = 10000
	maxBytesLimit   = 64 << 20
	maxLeaseLimit   = 1_000_000_000_000
	maxAttemptsLim  = 100
	maxIDLen        = 64
	maxPayloadLimit = 1 << 20
	minPriority     = -1000
	maxPriority     = 1000
)

type task struct {
	id         string
	deps       []string
	dependents []string
	priority   int
	payload    []byte
	result     []byte
	state      State
	attempt    uint32
	token      uint64
	deadline   int64
}

// Scheduler 是并发安全的内存 DAG 租约调度器。
type Scheduler struct {
	mu        sync.Mutex
	opts      Options
	tasks     map[string]*task
	ready     []string // 按 Priority 降序、ID 升序
	tokenSeq  uint64
	usedBytes int
}

func New(opts Options) (*Scheduler, error) {
	if opts.MaxTasks < 1 || opts.MaxTasks > maxTasksLimit ||
		opts.MaxBytes < 1 || opts.MaxBytes > maxBytesLimit ||
		opts.LeaseDuration < 1 || opts.LeaseDuration > maxLeaseLimit ||
		opts.MaxAttempts < 1 || opts.MaxAttempts > maxAttemptsLim {
		return nil, ErrInvalidOptions
	}
	return &Scheduler{opts: opts, tasks: make(map[string]*task)}, nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > maxIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validTime(now int64) bool { return now >= 0 && now <= MaxTime }

func (s *Scheduler) AddBatch(specs []TaskSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(specs) == 0 {
		return nil
	}
	// 1) 按输入顺序校验 ID、Priority、Payload、依赖 ID 与任务内重复依赖。
	for i := range specs {
		sp := &specs[i]
		if !validID(sp.ID) {
			return ErrInvalidID
		}
		if sp.Priority < minPriority || sp.Priority > maxPriority {
			return ErrInvalidPriority
		}
		if len(sp.Payload) > maxPayloadLimit {
			return ErrPayloadTooLarge
		}
		seen := make(map[string]struct{}, len(sp.Dependencies))
		for _, d := range sp.Dependencies {
			if !validID(d) {
				return ErrInvalidID
			}
			if _, ok := seen[d]; ok {
				return ErrDuplicate
			}
			seen[d] = struct{}{}
		}
	}
	// 2) 任务 ID 与已有/批内重复。
	batch := make(map[string]*task, len(specs))
	for i := range specs {
		id := specs[i].ID
		if _, ok := s.tasks[id]; ok {
			return ErrDuplicate
		}
		if _, ok := batch[id]; ok {
			return ErrDuplicate
		}
		batch[id] = &task{id: id}
	}
	// 3) 未知依赖（按输入及依赖顺序）。
	for i := range specs {
		for _, d := range specs[i].Dependencies {
			if _, ok := s.tasks[d]; !ok {
				if _, ok := batch[d]; !ok {
					return ErrUnknownDependency
				}
			}
		}
	}
	// 4) 环检测（仅需在新任务子图上搜索，已有图无环）。
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(specs))
	index := make(map[string]int, len(specs))
	for i := range specs {
		index[specs[i].ID] = i
	}
	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = gray
		for _, d := range specs[index[id]].Dependencies {
			if _, isNew := batch[d]; !isNew {
				continue
			}
			switch color[d] {
			case gray:
				return true
			case white:
				if visit(d) {
					return true
				}
			}
		}
		color[id] = black
		return false
	}
	for i := range specs {
		if color[specs[i].ID] == white && visit(specs[i].ID) {
			return ErrCycle
		}
	}
	// 5) 容量。
	if len(s.tasks)+len(specs) > s.opts.MaxTasks {
		return ErrCapacity
	}
	addBytes := 0
	for i := range specs {
		addBytes += len(specs[i].Payload)
	}
	if s.usedBytes+addBytes > s.opts.MaxBytes {
		return ErrCapacity
	}
	// 提交。
	for i := range specs {
		sp := &specs[i]
		t := batch[sp.ID]
		t.deps = slices.Clone(sp.Dependencies)
		t.priority = sp.Priority
		t.payload = slices.Clone(sp.Payload)
		t.state = StateReady
		for _, d := range t.deps {
			dep := s.tasks[d]
			if dep == nil {
				dep = batch[d]
			}
			dep.dependents = append(dep.dependents, t.id)
			if dep.state != StateSucceeded {
				t.state = StateBlocked
			}
		}
		s.tasks[t.id] = t
		if t.state == StateReady {
			s.readyInsert(t.id)
		}
	}
	s.usedBytes += addBytes
	return nil
}

// less 实现就绪顺序：Priority 降序，再按 ID 字节序升序。
func (s *Scheduler) less(a, b string) bool {
	ta, tb := s.tasks[a], s.tasks[b]
	if ta.priority != tb.priority {
		return ta.priority > tb.priority
	}
	return a < b
}

func (s *Scheduler) readyInsert(id string) {
	i := sort.Search(len(s.ready), func(i int) bool { return !s.less(s.ready[i], id) })
	s.ready = slices.Insert(s.ready, i, id)
}

func (s *Scheduler) readyRemove(id string) {
	if i := slices.Index(s.ready, id); i >= 0 {
		s.ready = slices.Delete(s.ready, i, i+1)
	}
}

func (s *Scheduler) Claim(now int64) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validTime(now) || now+s.opts.LeaseDuration > MaxTime {
		return Lease{}, ErrInvalidTime
	}
	if len(s.ready) == 0 {
		return Lease{}, ErrNoReady
	}
	id := s.ready[0]
	s.ready = s.ready[1:]
	t := s.tasks[id]
	t.state = StateRunning
	t.attempt++
	s.tokenSeq++
	t.token = s.tokenSeq
	t.deadline = now + s.opts.LeaseDuration
	return Lease{
		ID:       id,
		Token:    t.token,
		Attempt:  t.attempt,
		Deadline: t.deadline,
		Payload:  slices.Clone(t.payload),
	}, nil
}

func (s *Scheduler) Complete(now int64, id string, token uint64, result []byte, success bool) (Transition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validTime(now) {
		return Transition{}, ErrInvalidTime
	}
	if !validID(id) {
		return Transition{}, ErrInvalidID
	}
	t, ok := s.tasks[id]
	if !ok {
		return Transition{}, ErrUnknownTask
	}
	if t.state != StateRunning {
		return Transition{}, ErrNotRunning
	}
	if t.token != token || now >= t.deadline {
		return Transition{}, ErrStaleLease
	}
	if len(result) > maxPayloadLimit || (!success && result != nil) {
		return Transition{}, ErrInvalidResult
	}
	if success && s.usedBytes+len(result) > s.opts.MaxBytes {
		return Transition{}, ErrCapacity
	}
	var tr Transition
	if success {
		t.result = slices.Clone(result)
		t.state = StateSucceeded
		t.token, t.deadline = 0, 0
		s.usedBytes += len(result)
		var newly []string
		for _, depID := range t.dependents {
			d := s.tasks[depID]
			if d.state != StateBlocked {
				continue
			}
			allDone := true
			for _, dd := range d.deps {
				if s.tasks[dd].state != StateSucceeded {
					allDone = false
					break
				}
			}
			if allDone {
				d.state = StateReady
				s.readyInsert(d.id)
				newly = append(newly, d.id)
			}
		}
		sort.Slice(newly, func(i, j int) bool { return s.less(newly[i], newly[j]) })
		tr.Ready = newly
	} else {
		t.token, t.deadline = 0, 0
		_, failed, canceled := s.failTask(t)
		tr.Failed = failed
		tr.Canceled = canceled
	}
	return tr, nil
}

// failTask 处理一次失败：可重试则回到 ready，否则置 failed 并传递取消。
// 调用方须持有锁；t.token/deadline 须已清零。
func (s *Scheduler) failTask(t *task) (ready, failed, canceled []string) {
	if t.attempt < s.opts.MaxAttempts {
		t.state = StateReady
		s.readyInsert(t.id)
		return []string{t.id}, nil, nil
	}
	t.state = StateFailed
	failed = []string{t.id}
	canceled = s.cancelDependents(t.id)
	return nil, failed, canceled
}

// cancelDependents 取消 id 的所有非终态传递依赖者，返回按 ID 升序的列表。
func (s *Scheduler) cancelDependents(id string) []string {
	var canceled []string
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, depID := range s.tasks[cur].dependents {
			d := s.tasks[depID]
			switch d.state {
			case StateSucceeded, StateFailed, StateCanceled:
				continue
			}
			if d.state == StateReady {
				s.readyRemove(d.id)
			}
			d.state = StateCanceled
			d.token, d.deadline = 0, 0
			canceled = append(canceled, d.id)
			queue = append(queue, d.id)
		}
	}
	slices.Sort(canceled)
	return canceled
}

func (s *Scheduler) Sweep(now int64) (SweepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sr SweepResult
	if !validTime(now) {
		return sr, ErrInvalidTime
	}
	var expired []*task
	for _, t := range s.tasks {
		if t.state == StateRunning && t.deadline <= now {
			expired = append(expired, t)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].deadline != expired[j].deadline {
			return expired[i].deadline < expired[j].deadline
		}
		return expired[i].id < expired[j].id
	})
	var ready, failed, canceled []string
	for _, t := range expired {
		if t.state != StateRunning { // 可能已被先前终止级联取消
			continue
		}
		t.token, t.deadline = 0, 0
		sr.Expired = append(sr.Expired, t.id)
		r, f, c := s.failTask(t)
		ready = append(ready, r...)
		failed = append(failed, f...)
		canceled = append(canceled, c...)
	}
	sort.Slice(ready, func(i, j int) bool { return s.less(ready[i], ready[j]) })
	slices.Sort(failed)
	slices.Sort(canceled)
	sr.Ready = dedup(ready)
	sr.Failed = dedup(failed)
	sr.Canceled = dedup(canceled)
	return sr, nil
}

func dedup(ids []string) []string {
	out := ids[:0]
	var prev string
	for i, id := range ids {
		if i == 0 || id != prev {
			out = append(out, id)
			prev = id
		}
	}
	return out
}

func (s *Scheduler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{
		Tasks:     make([]TaskView, 0, len(s.tasks)),
		Ready:     slices.Clone(s.ready),
		UsedBytes: s.usedBytes,
	}
	for _, t := range s.tasks {
		v := TaskView{
			ID:           t.id,
			Dependencies: slices.Clone(t.deps),
			Priority:     t.priority,
			State:        t.state,
			Attempt:      t.attempt,
			Payload:      slices.Clone(t.payload),
			Result:       slices.Clone(t.result),
		}
		if t.state == StateRunning {
			v.LeaseToken = t.token
			v.Deadline = t.deadline
		}
		snap.Tasks = append(snap.Tasks, v)
	}
	sort.Slice(snap.Tasks, func(i, j int) bool { return snap.Tasks[i].ID < snap.Tasks[j].ID })
	return snap
}
