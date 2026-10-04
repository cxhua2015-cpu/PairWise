// Package breaker provides a concurrency-safe, in-memory circuit breaker
// registry driven by explicit time. See SPEC.md for the full contract.
package breaker

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrOpen           = errors.New("breaker open")
)

type Mode uint8

const (
	Closed Mode = iota + 1
	Open
	HalfOpen
)

type Options struct{ MaxServices, MaxNameBytes int }

type Policy struct {
	Name              string
	FailureThreshold  uint32
	RecoveryThreshold uint32
	OpenFor           int64
}

type Event struct {
	Service string
	Success bool
}

type Batch struct {
	Now    int64
	Events []Event
}

type Result struct {
	Generation uint64
	Changed    []string
}

type ServiceState struct {
	Name                        string
	Mode                        Mode
	Failures, RecoverySuccesses uint32
	OpenUntil                   int64
}

type Snapshot struct {
	Generation uint64
	Now        int64
	Services   []ServiceState
}

type service struct {
	policy Policy
	state  ServiceState
}

type Registry struct {
	mu         sync.Mutex
	maxNameLen int
	now        int64
	generation uint64
	names      []string // sorted
	services   map[string]*service
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func New(opts Options, policies []Policy) (*Registry, error) {
	if opts.MaxServices <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if len(policies) == 0 || len(policies) > opts.MaxServices {
		return nil, ErrInvalidOptions
	}
	seen := make(map[string]struct{}, len(policies))
	r := &Registry{
		maxNameLen: opts.MaxNameBytes,
		services:   make(map[string]*service, len(policies)),
	}
	for _, p := range policies {
		if !validName(p.Name, opts.MaxNameBytes) {
			return nil, ErrInvalidOptions
		}
		if p.FailureThreshold == 0 || p.RecoveryThreshold == 0 || p.OpenFor <= 0 {
			return nil, ErrInvalidOptions
		}
		if _, dup := seen[p.Name]; dup {
			return nil, ErrInvalidOptions
		}
		seen[p.Name] = struct{}{}
		r.services[p.Name] = &service{
			policy: p,
			state:  ServiceState{Name: p.Name, Mode: Closed},
		}
		r.names = append(r.names, p.Name)
	}
	sort.Strings(r.names)
	return r, nil
}

func openUntil(now, openFor int64) int64 {
	if openFor > math.MaxInt64-now {
		return math.MaxInt64
	}
	return now + openFor
}

// advanceLocked moves every due Open service to HalfOpen, resetting counters.
// Returns names of transitioned services in sorted order.
func (r *Registry) advanceLocked(now int64) []string {
	var changed []string
	for _, name := range r.names {
		s := r.services[name]
		if s.state.Mode == Open && now >= s.state.OpenUntil {
			s.state.Mode = HalfOpen
			s.state.Failures = 0
			s.state.RecoverySuccesses = 0
			changed = append(changed, name)
		}
	}
	return changed
}

// applyLocked applies one event to a cloned state map view. It returns the
// service name if the event caused a mode transition.
func applyEvent(s *service, now int64, success bool) (bool, error) {
	st := &s.state
	switch st.Mode {
	case Open:
		return false, ErrOpen
	case Closed:
		if success {
			st.Failures = 0
			return false, nil
		}
		st.Failures++
		if st.Failures >= s.policy.FailureThreshold {
			st.Mode = Open
			st.OpenUntil = openUntil(now, s.policy.OpenFor)
			st.Failures = 0
			st.RecoverySuccesses = 0
			return true, nil
		}
		return false, nil
	case HalfOpen:
		if !success {
			st.Mode = Open
			st.OpenUntil = openUntil(now, s.policy.OpenFor)
			st.Failures = 0
			st.RecoverySuccesses = 0
			return true, nil
		}
		st.RecoverySuccesses++
		if st.RecoverySuccesses >= s.policy.RecoveryThreshold {
			st.Mode = Closed
			st.Failures = 0
			st.RecoverySuccesses = 0
			return true, nil
		}
		return false, nil
	}
	return false, nil
}

func (r *Registry) Record(b Batch) (Result, error) {
	// Phase 1: structural validation, no state reads.
	for _, e := range b.Events {
		if !validName(e.Service, r.maxNameLen) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Phase 2: time monotonicity.
	if b.Now < r.now {
		return Result{}, ErrTime
	}

	// Phase 3: work on an isolated candidate; commit only on success.
	type backup struct {
		s  *service
		st ServiceState
	}
	var touched []backup
	save := func(s *service) {
		touched = append(touched, backup{s, s.state})
	}

	changedSet := make(map[string]struct{})
	// Automatic transitions on candidate state.
	for _, name := range r.names {
		s := r.services[name]
		if s.state.Mode == Open && b.Now >= s.state.OpenUntil {
			save(s)
			s.state.Mode = HalfOpen
			s.state.Failures = 0
			s.state.RecoverySuccesses = 0
			changedSet[name] = struct{}{}
		}
	}

	rollback := func(err error) (Result, error) {
		for i := len(touched) - 1; i >= 0; i-- {
			*touched[i].s = service{policy: touched[i].s.policy, state: touched[i].st}
		}
		return Result{}, err
	}

	savedSet := make(map[string]struct{})
	for _, bk := range touched {
		savedSet[bk.s.state.Name] = struct{}{}
	}

	for _, e := range b.Events {
		s, ok := r.services[e.Service]
		if !ok {
			return rollback(ErrNotFound)
		}
		if _, done := savedSet[e.Service]; !done {
			save(s)
			savedSet[e.Service] = struct{}{}
		}
		transitioned, err := applyEvent(s, b.Now, e.Success)
		if err != nil {
			return rollback(err)
		}
		if transitioned {
			changedSet[e.Service] = struct{}{}
		}
	}

	// Commit.
	r.now = b.Now
	if len(b.Events) > 0 || len(changedSet) > 0 {
		r.generation++
	}
	changed := make([]string, 0, len(changedSet))
	for name := range changedSet {
		changed = append(changed, name)
	}
	sort.Strings(changed)
	return Result{Generation: r.generation, Changed: changed}, nil
}

func (r *Registry) Allow(service string, now int64) (bool, ServiceState, error) {
	if !validName(service, r.maxNameLen) {
		return false, ServiceState{}, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return false, ServiceState{}, ErrTime
	}
	changed := r.advanceLocked(now)
	r.now = now
	if len(changed) > 0 {
		r.generation++
	}
	s, ok := r.services[service]
	if !ok {
		return false, ServiceState{}, ErrNotFound
	}
	st := s.state
	return st.Mode == Closed || st.Mode == HalfOpen, st, nil
}

func (r *Registry) Sweep(now int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return nil, ErrTime
	}
	changed := r.advanceLocked(now)
	r.now = now
	if len(changed) > 0 {
		r.generation++
	}
	return changed, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := Snapshot{
		Generation: r.generation,
		Now:        r.now,
		Services:   make([]ServiceState, 0, len(r.names)),
	}
	for _, name := range r.names {
		snap.Services = append(snap.Services, r.services[name].state)
	}
	return snap
}
