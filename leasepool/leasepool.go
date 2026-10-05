package leasepool

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrBusy           = errors.New("busy")
	ErrOwner          = errors.New("owner mismatch")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Add Kind = iota + 1
	Remove
	Acquire
	Renew
	Release
)

type Options struct{ MaxResources, MaxNameBytes, MaxOwnerBytes int }
type Op struct {
	Kind            Kind
	Resource, Owner string
	ExpiresAt       int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Lease struct {
	Resource, Owner string
	ExpiresAt       int64
	Revision        uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Lease
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Resources                []string
	Leases                   []Lease
}

type Pool struct {
	mu         sync.Mutex
	maxRes     int
	maxName    int
	maxOwner   int
	now        int64
	generation uint64
	revision   uint64
	resources  map[string]struct{}
	leases     map[string]Lease
}

func New(o Options) (*Pool, error) {
	if o.MaxResources <= 0 || o.MaxNameBytes <= 0 || o.MaxOwnerBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Pool{
		maxRes:    o.MaxResources,
		maxName:   o.MaxNameBytes,
		maxOwner:  o.MaxOwnerBytes,
		resources: make(map[string]struct{}),
		leases:    make(map[string]Lease),
	}, nil
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
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

func (p *Pool) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validName(op.Resource, p.maxName) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add, Remove:
			if op.Owner != "" || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		case Acquire, Renew:
			if !validName(op.Owner, p.maxOwner) || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Release:
			if !validName(op.Owner, p.maxOwner) || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (p *Pool) Apply(b Batch) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < p.now {
		return Result{}, ErrTime
	}

	// Isolated candidate state.
	resources := make(map[string]struct{}, len(p.resources))
	for r := range p.resources {
		resources[r] = struct{}{}
	}
	leases := make(map[string]Lease, len(p.leases))
	for r, l := range p.leases {
		leases[r] = l
	}

	revision := p.revision
	changed := make(map[string]Lease)

	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if _, ok := resources[op.Resource]; ok {
				return Result{}, ErrExists
			}
			revision++
			resources[op.Resource] = struct{}{}
		case Remove:
			if _, ok := resources[op.Resource]; !ok {
				return Result{}, ErrNotFound
			}
			if _, ok := leases[op.Resource]; ok {
				return Result{}, ErrBusy
			}
			revision++
			delete(resources, op.Resource)
		case Acquire:
			if _, ok := resources[op.Resource]; !ok {
				return Result{}, ErrNotFound
			}
			if l, ok := leases[op.Resource]; ok && l.ExpiresAt > b.Now {
				return Result{}, ErrBusy
			}
			revision++
			l := Lease{Resource: op.Resource, Owner: op.Owner, ExpiresAt: op.ExpiresAt, Revision: revision}
			leases[op.Resource] = l
			changed[op.Resource] = l
		case Renew:
			l, ok := leases[op.Resource]
			if !ok || l.ExpiresAt <= b.Now {
				return Result{}, ErrNotFound
			}
			if l.Owner != op.Owner {
				return Result{}, ErrOwner
			}
			revision++
			l = Lease{Resource: op.Resource, Owner: op.Owner, ExpiresAt: op.ExpiresAt, Revision: revision}
			leases[op.Resource] = l
			changed[op.Resource] = l
		case Release:
			l, ok := leases[op.Resource]
			if !ok || l.ExpiresAt <= b.Now {
				return Result{}, ErrNotFound
			}
			if l.Owner != op.Owner {
				return Result{}, ErrOwner
			}
			revision++
			delete(leases, op.Resource)
			delete(changed, op.Resource)
		}
	}

	if len(resources) > p.maxRes {
		return Result{}, ErrCapacity
	}

	// Commit.
	p.resources = resources
	p.leases = leases
	p.revision = revision
	p.now = b.Now
	if len(b.Ops) > 0 {
		p.generation++
	}

	res := Result{Generation: p.generation}
	if len(b.Ops) > 0 {
		res.Revision = revision
	}
	if len(changed) > 0 {
		names := make([]string, 0, len(changed))
		for r := range changed {
			names = append(names, r)
		}
		sort.Strings(names)
		res.Changed = make([]Lease, 0, len(names))
		for _, r := range names {
			res.Changed = append(res.Changed, changed[r])
		}
	}
	return res, nil
}

func (p *Pool) Expire(now int64) ([]Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now < 0 {
		return nil, ErrInvalidInput
	}
	if now < p.now {
		return nil, ErrTime
	}
	var gone []Lease
	for r, l := range p.leases {
		if l.ExpiresAt <= now {
			gone = append(gone, l)
			delete(p.leases, r)
		}
	}
	p.now = now
	if len(gone) > 0 {
		p.generation++
		sort.Slice(gone, func(i, j int) bool { return gone[i].Resource < gone[j].Resource })
	}
	return gone, nil
}

func (p *Pool) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Snapshot{
		Generation:   p.generation,
		NextRevision: p.revision + 1,
		Now:          p.now,
		Resources:    make([]string, 0, len(p.resources)),
		Leases:       make([]Lease, 0, len(p.leases)),
	}
	for r := range p.resources {
		s.Resources = append(s.Resources, r)
	}
	for _, l := range p.leases {
		s.Leases = append(s.Leases, l)
	}
	sort.Strings(s.Resources)
	sort.Slice(s.Leases, func(i, j int) bool { return s.Leases[i].Resource < s.Leases[j].Resource })
	return s
}
