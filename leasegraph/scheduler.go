package leasegraph

import (
	"sort"
	"sync"
)

const (
	maxTasksLimit    = 10000
	maxBytesLimit    = 64 << 20
	maxLeaseDuration = 1_000_000_000_000
	maxAttemptsLimit = 100
	maxIDLen         = 64
	maxPayloadLen    = 1 << 20
	minPriority      = -1000
	maxPriority      = 1000
)

type task struct {
	id       string
	deps     []string
	priority int
	payload  []byte
	result   []byte
	state    State
	attempt  uint32
	token    uint64
	deadline int64
}

func isTerminal(s State) bool {
	return s == StateSucceeded || s == StateFailed || s == StateCanceled
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > maxIDLen {
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

func validTime(now int64) bool {
	return now >= 0 && now <= MaxTime
}

// Scheduler is a concurrency-safe in-memory DAG lease scheduler.
// All public methods may be called from multiple goroutines.
type Scheduler struct {
	mu         sync.Mutex
	maxTasks   int
	maxBytes   int
	leaseDur   int64
	maxAttempt uint32

	tasks      map[string]*task
	dependents map[string][]string
	usedBytes  int
	nextToken  uint64
}

func New(opts Options) (*Scheduler, error) {
	if opts.MaxTasks < 1 || opts.MaxTasks > maxTasksLimit ||
		opts.MaxBytes < 1 || opts.MaxBytes > maxBytesLimit ||
		opts.LeaseDuration < 1 || opts.LeaseDuration > maxLeaseDuration ||
		opts.MaxAttempts < 1 || opts.MaxAttempts > maxAttemptsLimit {
		return nil, ErrInvalidOptions
	}
	return &Scheduler{
		maxTasks:   opts.MaxTasks,
		maxBytes:   opts.MaxBytes,
		leaseDur:   opts.LeaseDuration,
		maxAttempt: opts.MaxAttempts,
		tasks:      make(map[string]*task),
		dependents: make(map[string][]string),
		nextToken:  1,
	}, nil
}

// readyLess orders tasks by priority descending, then ID ascending.
func readyLess(a, b *task) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	return a.id < b.id
}

func sortReady(tasks []*task) {
	sort.Slice(tasks, func(i, j int) bool { return readyLess(tasks[i], tasks[j]) })
}

func (s *Scheduler) AddBatch(specs []TaskSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(specs) == 0 {
		return nil
	}

	// Phase 1: per-task field validation in input order.
	for i := range specs {
		spec := &specs[i]
		if !validID(spec.ID) {
			return ErrInvalidID
		}
		if spec.Priority < minPriority || spec.Priority > maxPriority {
			return ErrInvalidPriority
		}
		if len(spec.Payload) > maxPayloadLen {
			return ErrPayloadTooLarge
		}
		seenDeps := make(map[string]struct{}, len(spec.Dependencies))
		for _, dep := range spec.Dependencies {
			if !validID(dep) {
				return ErrInvalidID
			}
			if _, ok := seenDeps[dep]; ok {
				return ErrDuplicate
			}
			seenDeps[dep] = struct{}{}
		}
	}

	// Phase 2: duplicate task IDs against existing tasks and within the batch.
	batchIDs := make(map[string]struct{}, len(specs))
	for i := range specs {
		id := specs[i].ID
		if _, ok := s.tasks[id]; ok {
			return ErrDuplicate
		}
		if _, ok := batchIDs[id]; ok {
			return ErrDuplicate
		}
		batchIDs[id] = struct{}{}
	}

	// Phase 3: unknown dependencies, in input and dependency order.
	for i := range specs {
		for _, dep := range specs[i].Dependencies {
			if _, ok := s.tasks[dep]; ok {
				continue
			}
			if _, ok := batchIDs[dep]; ok {
				continue
			}
			return ErrUnknownDependency
		}
	}

	// Phase 4: cycle detection. The committed graph is acyclic, so any cycle
	// lives entirely inside the batch subgraph; run Kahn's algorithm on it.
	inDegree := make(map[string]int, len(specs))
	batchDependents := make(map[string][]string, len(specs))
	for i := range specs {
		inDegree[specs[i].ID] = 0
	}
	for i := range specs {
		for _, dep := range specs[i].Dependencies {
			if _, ok := batchIDs[dep]; ok {
				inDegree[specs[i].ID]++
				batchDependents[dep] = append(batchDependents[dep], specs[i].ID)
			}
		}
	}
	queue := make([]string, 0, len(specs))
	for i := range specs {
		if inDegree[specs[i].ID] == 0 {
			queue = append(queue, specs[i].ID)
		}
	}
	resolved := 0
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		resolved++
		for _, dependent := range batchDependents[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if resolved != len(specs) {
		return ErrCycle
	}

	// Phase 5: capacity limits.
	if len(s.tasks)+len(specs) > s.maxTasks {
		return ErrCapacity
	}
	addBytes := 0
	for i := range specs {
		addBytes += len(specs[i].Payload)
	}
	if s.usedBytes+addBytes > s.maxBytes {
		return ErrCapacity
	}

	// Commit. Batch tasks are never succeeded at commit time, so a new task
	// is ready iff every dependency resolves to an existing succeeded task.
	for i := range specs {
		spec := &specs[i]
		deps := make([]string, len(spec.Dependencies))
		copy(deps, spec.Dependencies)
		var payload []byte
		if spec.Payload != nil {
			payload = make([]byte, len(spec.Payload))
			copy(payload, spec.Payload)
		}
		t := &task{
			id:       spec.ID,
			deps:     deps,
			priority: spec.Priority,
			payload:  payload,
			state:    StateReady,
		}
		for _, dep := range deps {
			if depTask := s.tasks[dep]; depTask == nil || depTask.state != StateSucceeded {
				t.state = StateBlocked
			}
			s.dependents[dep] = append(s.dependents[dep], t.id)
		}
		s.tasks[t.id] = t
		s.usedBytes += len(payload)
	}
	return nil
}

func (s *Scheduler) Claim(now int64) (Lease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !validTime(now) || now+s.leaseDur > MaxTime {
		return Lease{}, ErrInvalidTime
	}
	var best *task
	for _, t := range s.tasks {
		if t.state != StateReady {
			continue
		}
		if best == nil || readyLess(t, best) {
			best = t
		}
	}
	if best == nil {
		return Lease{}, ErrNoReady
	}
	best.state = StateRunning
	best.attempt++
	best.token = s.nextToken
	s.nextToken++
	best.deadline = now + s.leaseDur
	var payload []byte
	if best.payload != nil {
		payload = make([]byte, len(best.payload))
		copy(payload, best.payload)
	}
	return Lease{
		ID:       best.id,
		Token:    best.token,
		Attempt:  best.attempt,
		Deadline: best.deadline,
		Payload:  payload,
	}, nil
}

// cancelDependents marks every transitive dependent of id that is not in a
// terminal state as canceled and returns the canceled IDs.
func (s *Scheduler) cancelDependents(id string, canceled map[string]struct{}) {
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, dependent := range s.dependents[cur] {
			if _, ok := canceled[dependent]; ok {
				continue
			}
			t := s.tasks[dependent]
			if isTerminal(t.state) {
				continue
			}
			t.state = StateCanceled
			t.token = 0
			t.deadline = 0
			canceled[dependent] = struct{}{}
			queue = append(queue, dependent)
		}
	}
}

// failTask applies the retry/terminate rule for a running task without
// incrementing its attempt counter. It appends state changes to the
// provided collectors.
func (s *Scheduler) failTask(t *task, ready, failed *[]string, canceled map[string]struct{}) {
	t.token = 0
	t.deadline = 0
	if t.attempt < s.maxAttempt {
		t.state = StateReady
		*ready = append(*ready, t.id)
		return
	}
	t.state = StateFailed
	*failed = append(*failed, t.id)
	s.cancelDependents(t.id, canceled)
}

// succeedTask marks t succeeded and unblocks direct dependents whose
// dependencies are all succeeded, returning their IDs in ready order.
func (s *Scheduler) succeedTask(t *task) []string {
	t.state = StateSucceeded
	t.token = 0
	t.deadline = 0
	var unblocked []*task
	for _, dependent := range s.dependents[t.id] {
		dt := s.tasks[dependent]
		if dt.state != StateBlocked {
			continue
		}
		allSucceeded := true
		for _, dep := range dt.deps {
			if s.tasks[dep].state != StateSucceeded {
				allSucceeded = false
				break
			}
		}
		if allSucceeded {
			dt.state = StateReady
			unblocked = append(unblocked, dt)
		}
	}
	sortReady(unblocked)
	ids := make([]string, len(unblocked))
	for i, dt := range unblocked {
		ids[i] = dt.id
	}
	return ids
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
	if t.token != token {
		return Transition{}, ErrStaleLease
	}
	if now >= t.deadline {
		return Transition{}, ErrStaleLease
	}
	if success {
		if len(result) > maxPayloadLen {
			return Transition{}, ErrInvalidResult
		}
	} else if result != nil {
		return Transition{}, ErrInvalidResult
	}
	if success && s.usedBytes+len(result) > s.maxBytes {
		return Transition{}, ErrCapacity
	}

	tr := Transition{}
	if success {
		if result != nil {
			saved := make([]byte, len(result))
			copy(saved, result)
			t.result = saved
			s.usedBytes += len(saved)
		}
		tr.Ready = s.succeedTask(t)
		return tr, nil
	}

	canceled := make(map[string]struct{})
	var ready, failed []string
	s.failTask(t, &ready, &failed, canceled)
	tr.Ready = ready
	tr.Failed = failed
	tr.Canceled = sortedKeys(canceled)
	return tr, nil
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Scheduler) Sweep(now int64) (SweepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !validTime(now) {
		return SweepResult{}, ErrInvalidTime
	}
	var expired []*task
	for _, t := range s.tasks {
		if t.state == StateRunning && t.deadline <= now {
			expired = append(expired, t)
		}
	}
	res := SweepResult{}
	if len(expired) == 0 {
		return res, nil
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].deadline != expired[j].deadline {
			return expired[i].deadline < expired[j].deadline
		}
		return expired[i].id < expired[j].id
	})
	canceled := make(map[string]struct{})
	var ready, failed []string
	for _, t := range expired {
		if t.state != StateRunning {
			// Canceled by an earlier cascade in this sweep.
			continue
		}
		res.Expired = append(res.Expired, t.id)
		s.failTask(t, &ready, &failed, canceled)
	}
	res.Ready = ready
	res.Failed = failed
	res.Canceled = sortedKeys(canceled)
	return res, nil
}

func (s *Scheduler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{UsedBytes: s.usedBytes}
	ids := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var ready []*task
	for _, id := range ids {
		t := s.tasks[id]
		view := TaskView{
			ID:           t.id,
			Priority:     t.priority,
			State:        t.state,
			Attempt:      t.attempt,
			Dependencies: append([]string(nil), t.deps...),
		}
		if t.state == StateRunning {
			view.LeaseToken = t.token
			view.Deadline = t.deadline
		}
		if t.payload != nil {
			view.Payload = append([]byte(nil), t.payload...)
		}
		if t.result != nil {
			view.Result = append([]byte(nil), t.result...)
		}
		snap.Tasks = append(snap.Tasks, view)
		if t.state == StateReady {
			ready = append(ready, t)
		}
	}
	sortReady(ready)
	for _, t := range ready {
		snap.Ready = append(snap.Ready, t.id)
	}
	return snap
}
