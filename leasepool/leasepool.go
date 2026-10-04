package leasepool

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxPools, MaxLeases, MaxNameBytes, MaxOwnerBytes int }
type PoolConfig struct {
	Name     string
	Capacity uint64
}
type OpKind uint8

const (
	Acquire OpKind = iota + 1
	Release
	Renew
)

type Op struct {
	Kind                 OpKind
	Pool, LeaseID, Owner string
	Weight               uint64
	ExpiresAt            int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Result struct {
	Generation uint64
	Expired    []string
}
type Lease struct {
	Pool, LeaseID, Owner string
	Weight               uint64
	ExpiresAt            int64
}
type PoolState struct {
	Name           string
	Capacity, Used uint64
	Leases         int
}
type Snapshot struct {
	Generation uint64
	Now        int64
	Pools      []PoolState
	Leases     []Lease
}

type pool struct {
	name     string
	capacity uint64
	used     uint64
	leases   int
}

type Registry struct {
	mu            sync.Mutex
	maxLeases     int
	maxNameBytes  int
	maxOwnerBytes int
	pools         map[string]*pool
	leases        map[string]*Lease
	now           int64
	generation    uint64
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func New(o Options, cfgs []PoolConfig) (*Registry, error) {
	if o.MaxPools <= 0 || o.MaxLeases <= 0 || o.MaxNameBytes <= 0 || o.MaxOwnerBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	if len(cfgs) < 1 || len(cfgs) > o.MaxPools {
		return nil, ErrInvalidOptions
	}
	r := &Registry{
		maxLeases:     o.MaxLeases,
		maxNameBytes:  o.MaxNameBytes,
		maxOwnerBytes: o.MaxOwnerBytes,
		pools:         make(map[string]*pool, len(cfgs)),
		leases:        make(map[string]*Lease),
	}
	for _, c := range cfgs {
		if !validName(c.Name, o.MaxNameBytes) || c.Capacity == 0 {
			return nil, ErrInvalidOptions
		}
		if _, dup := r.pools[c.Name]; dup {
			return nil, ErrInvalidOptions
		}
		r.pools[c.Name] = &pool{name: c.Name, capacity: c.Capacity}
	}
	return r, nil
}

func (r *Registry) validateOp(op Op, now int64) bool {
	switch op.Kind {
	case Acquire:
		return validName(op.Pool, r.maxNameBytes) && validName(op.LeaseID, r.maxNameBytes) &&
			validName(op.Owner, r.maxOwnerBytes) && op.Weight > 0 && op.ExpiresAt > now
	case Release:
		return validName(op.LeaseID, r.maxNameBytes) && op.Pool == "" && op.Owner == "" &&
			op.Weight == 0 && op.ExpiresAt == 0
	case Renew:
		return validName(op.LeaseID, r.maxNameBytes) && op.Pool == "" && op.Owner == "" &&
			op.Weight == 0 && op.ExpiresAt > now
	}
	return false
}

// expireLocked removes all leases with ExpiresAt <= now, returning sorted IDs.
func expireLocked(pools map[string]*pool, leases map[string]*Lease, now int64) []string {
	var expired []string
	for id, l := range leases {
		if l.ExpiresAt <= now {
			expired = append(expired, id)
		}
	}
	sort.Strings(expired)
	for _, id := range expired {
		l := leases[id]
		p := pools[l.Pool]
		p.used -= l.Weight
		p.leases--
		delete(leases, id)
	}
	return expired
}

func (r *Registry) Apply(b Batch) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Phase 1: structural validation of every op, without reading state.
	for _, op := range b.Ops {
		if !r.validateOp(op, b.Now) {
			return Result{}, ErrInvalidInput
		}
	}
	// Phase 2: global time monotonicity.
	if b.Now < r.now {
		return Result{}, ErrTime
	}

	// Phase 3: isolated candidate state.
	pools := make(map[string]*pool, len(r.pools))
	for name, p := range r.pools {
		cp := *p
		pools[name] = &cp
	}
	leases := make(map[string]*Lease, len(r.leases))
	for id, l := range r.leases {
		cp := *l
		leases[id] = &cp
	}

	expired := expireLocked(pools, leases, b.Now)

	for _, op := range b.Ops {
		switch op.Kind {
		case Acquire:
			p, ok := pools[op.Pool]
			if !ok {
				return Result{}, ErrNotFound
			}
			if _, dup := leases[op.LeaseID]; dup {
				return Result{}, ErrConflict
			}
			if len(leases) >= r.maxLeases {
				return Result{}, ErrCapacity
			}
			if op.Weight > p.capacity-p.used {
				return Result{}, ErrCapacity
			}
			p.used += op.Weight
			p.leases++
			leases[op.LeaseID] = &Lease{
				Pool: op.Pool, LeaseID: op.LeaseID, Owner: op.Owner,
				Weight: op.Weight, ExpiresAt: op.ExpiresAt,
			}
		case Release:
			l, ok := leases[op.LeaseID]
			if !ok {
				return Result{}, ErrNotFound
			}
			p := pools[l.Pool]
			p.used -= l.Weight
			p.leases--
			delete(leases, op.LeaseID)
		case Renew:
			l, ok := leases[op.LeaseID]
			if !ok {
				return Result{}, ErrNotFound
			}
			l.ExpiresAt = op.ExpiresAt
		}
	}

	// Commit.
	r.pools = pools
	r.leases = leases
	r.now = b.Now
	if len(expired) > 0 || len(b.Ops) > 0 {
		r.generation++
	}
	return Result{Generation: r.generation, Expired: expired}, nil
}

func (r *Registry) Sweep(now int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return nil, ErrTime
	}
	expired := expireLocked(r.pools, r.leases, now)
	r.now = now
	if len(expired) > 0 {
		r.generation++
	}
	return expired, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		Generation: r.generation,
		Now:        r.now,
		Pools:      make([]PoolState, 0, len(r.pools)),
		Leases:     make([]Lease, 0, len(r.leases)),
	}
	for _, p := range r.pools {
		s.Pools = append(s.Pools, PoolState{
			Name: p.name, Capacity: p.capacity, Used: p.used, Leases: p.leases,
		})
	}
	sort.Slice(s.Pools, func(i, j int) bool { return s.Pools[i].Name < s.Pools[j].Name })
	for _, l := range r.leases {
		s.Leases = append(s.Leases, *l)
	}
	sort.Slice(s.Leases, func(i, j int) bool { return s.Leases[i].LeaseID < s.Leases[j].LeaseID })
	return s
}
