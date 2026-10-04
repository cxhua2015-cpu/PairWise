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
	Kind      OpKind
	Pool      string
	LeaseID   string
	Owner     string
	Weight    uint64
	ExpiresAt int64
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
	Pool      string
	LeaseID   string
	Owner     string
	Weight    uint64
	ExpiresAt int64
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
	capacity uint64
	used     uint64
	leases   int
}

type Registry struct {
	mu     sync.Mutex
	opts   Options
	now    int64
	gen    uint64
	pools  map[string]*pool
	leases map[string]*Lease
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '/' || c == '-':
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
	r := &Registry{opts: o, pools: make(map[string]*pool, len(cfgs)), leases: make(map[string]*Lease)}
	for _, c := range cfgs {
		if !validName(c.Name, o.MaxNameBytes) || c.Capacity == 0 {
			return nil, ErrInvalidOptions
		}
		if _, dup := r.pools[c.Name]; dup {
			return nil, ErrInvalidOptions
		}
		r.pools[c.Name] = &pool{capacity: c.Capacity}
	}
	return r, nil
}

func (r *Registry) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Acquire:
			if !validName(op.Pool, r.opts.MaxNameBytes) ||
				!validName(op.LeaseID, r.opts.MaxNameBytes) ||
				!validName(op.Owner, r.opts.MaxOwnerBytes) ||
				op.Weight == 0 || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Release:
			if !validName(op.LeaseID, r.opts.MaxNameBytes) ||
				op.Pool != "" || op.Owner != "" || op.Weight != 0 || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		case Renew:
			if !validName(op.LeaseID, r.opts.MaxNameBytes) ||
				op.Pool != "" || op.Owner != "" || op.Weight != 0 ||
				op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func clonePools(src map[string]*pool) map[string]*pool {
	dst := make(map[string]*pool, len(src))
	for k, v := range src {
		c := *v
		dst[k] = &c
	}
	return dst
}

func cloneLeases(src map[string]*Lease) map[string]*Lease {
	dst := make(map[string]*Lease, len(src))
	for k, v := range src {
		c := *v
		dst[k] = &c
	}
	return dst
}

// expireDue removes leases with ExpiresAt <= now from the candidate state and
// returns their IDs sorted lexicographically.
func expireDue(pools map[string]*pool, leases map[string]*Lease, now int64) []string {
	var expired []string
	for id, l := range leases {
		if l.ExpiresAt <= now {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		l := leases[id]
		p := pools[l.Pool]
		p.used -= l.Weight
		p.leases--
		delete(leases, id)
	}
	sort.Strings(expired)
	return expired
}

func (r *Registry) Apply(b Batch) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < r.now {
		return Result{}, ErrTime
	}

	pools := clonePools(r.pools)
	leases := cloneLeases(r.leases)
	expired := expireDue(pools, leases, b.Now)

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
			if len(leases) >= r.opts.MaxLeases {
				return Result{}, ErrCapacity
			}
			if op.Weight > p.capacity-p.used {
				return Result{}, ErrCapacity
			}
			p.used += op.Weight
			p.leases++
			leases[op.LeaseID] = &Lease{Pool: op.Pool, LeaseID: op.LeaseID, Owner: op.Owner, Weight: op.Weight, ExpiresAt: op.ExpiresAt}
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

	r.pools = pools
	r.leases = leases
	r.now = b.Now
	if len(expired) > 0 || len(b.Ops) > 0 {
		r.gen++
	}
	return Result{Generation: r.gen, Expired: expired}, nil
}

func (r *Registry) Sweep(now int64) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now < r.now {
		return nil, ErrTime
	}
	pools := clonePools(r.pools)
	leases := cloneLeases(r.leases)
	expired := expireDue(pools, leases, now)
	r.pools = pools
	r.leases = leases
	r.now = now
	if len(expired) > 0 {
		r.gen++
	}
	return expired, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{Generation: r.gen, Now: r.now}
	names := make([]string, 0, len(r.pools))
	for n := range r.pools {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := r.pools[n]
		s.Pools = append(s.Pools, PoolState{Name: n, Capacity: p.capacity, Used: p.used, Leases: p.leases})
	}
	ids := make([]string, 0, len(r.leases))
	for id := range r.leases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.Leases = append(s.Leases, *r.leases[id])
	}
	return s
}
