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

// Registry is a concurrency-safe circuit breaker registry.
type Registry struct {
	mu         sync.Mutex
	maxNameLen int
	now        int64
	generation uint64
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
	}
	return r, nil
}

func saturatingAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// advanceLocked moves every due Open service to HalfOpen and records changes.
func (r *Registry) advanceLocked(now int64, changed map[string]struct{}) {
	for _, s := range r.services {
		if s.state.Mode == Open && now >= s.state.OpenUntil {
			s.state.Mode = HalfOpen
			s.state.Failures = 0
			s.state.RecoverySuccesses = 0
			s.state.OpenUntil = 0
			changed[s.state.Name] = struct{}{}
		}
	}
	r.now = now
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Record(b Batch) (Result, error) {
	// Structural validation of all event names, in input order, without
	// reading any state.
	for _, e := range b.Events {
		if !validName(e.Service, r.maxNameLen) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if b.Now < r.now {
		return Result{}, ErrTime
	}

	// Clone states so any failure rolls back completely.
	clone := make(map[string]ServiceState, len(r.services))
	for name, s := range r.services {
		clone[name] = s.state
	}
	prevNow := r.now

	changed := make(map[string]struct{})
	r.advanceLocked(b.Now, changed)

	for _, e := range b.Events {
		s, ok := r.services[e.Service]
		if !ok {
			r.rollbackLocked(clone, prevNow)
			return Result{}, ErrNotFound
		}
		st := &s.state
		switch st.Mode {
		case Open:
			r.rollbackLocked(clone, prevNow)
			return Result{}, ErrOpen
		case Closed:
			if e.Success {
				st.Failures = 0
			} else {
				st.Failures++
				if st.Failures >= s.policy.FailureThreshold {
					st.Mode = Open
					st.OpenUntil = saturatingAdd(b.Now, s.policy.OpenFor)
					st.Failures = 0
					st.RecoverySuccesses = 0
					changed[st.Name] = struct{}{}
				}
			}
		case HalfOpen:
			if e.Success {
				st.RecoverySuccesses++
				if st.RecoverySuccesses >= s.policy.RecoveryThreshold {
					st.Mode = Closed
					st.Failures = 0
					st.RecoverySuccesses = 0
					changed[st.Name] = struct{}{}
				}
			} else {
				st.Mode = Open
				st.OpenUntil = saturatingAdd(b.Now, s.policy.OpenFor)
				st.Failures = 0
				st.RecoverySuccesses = 0
				changed[st.Name] = struct{}{}
			}
		}
	}

	if len(b.Events) > 0 || len(changed) > 0 {
		r.generation++
	}
	return Result{Generation: r.generation, Changed: sortedKeys(changed)}, nil
}

func (r *Registry) rollbackLocked(clone map[string]ServiceState, prevNow int64) {
	for name, st := range clone {
		r.services[name].state = st
	}
	r.now = prevNow
}

func (r *Registry) Allow(name string, now int64) (bool, ServiceState, error) {
	if !validName(name, r.maxNameLen) {
		return false, ServiceState{}, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return false, ServiceState{}, ErrTime
	}
	changed := make(map[string]struct{})
	r.advanceLocked(now, changed)
	if len(changed) > 0 {
		r.generation++
	}
	s, ok := r.services[name]
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
	changed := make(map[string]struct{})
	r.advanceLocked(now, changed)
	if len(changed) > 0 {
		r.generation++
	}
	return sortedKeys(changed), nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := Snapshot{
		Generation: r.generation,
		Now:        r.now,
		Services:   make([]ServiceState, 0, len(r.services)),
	}
	for _, s := range r.services {
		snap.Services = append(snap.Services, s.state)
	}
	sort.Slice(snap.Services, func(i, j int) bool {
		return snap.Services[i].Name < snap.Services[j].Name
	})
	return snap
}
