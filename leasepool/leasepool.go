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

type state struct {
	now        int64
	generation uint64
	revision   uint64
	resources  map[string]struct{}
	leases     map[string]Lease
}

type Pool struct {
	mu sync.Mutex
	st state
	mr int
	mn int
	mo int
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

func New(o Options) (*Pool, error) {
	if o.MaxResources <= 0 || o.MaxNameBytes <= 0 || o.MaxOwnerBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Pool{
		st: state{resources: map[string]struct{}{}, leases: map[string]Lease{}},
		mr: o.MaxResources, mn: o.MaxNameBytes, mo: o.MaxOwnerBytes,
	}, nil
}

func (p *Pool) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validName(op.Resource, p.mn) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add, Remove:
			if op.Owner != "" || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		case Acquire, Renew:
			if !validName(op.Owner, p.mo) || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Release:
			if !validName(op.Owner, p.mo) || op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (p *Pool) Apply(b Batch) (Result, error) {
	if err := p.validate(b); err != nil {
		return Result{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if b.Now < p.st.now {
		return Result{}, ErrTime
	}
	// Isolated candidate state.
	cand := state{
		now:        p.st.now,
		generation: p.st.generation,
		revision:   p.st.revision,
		resources:  make(map[string]struct{}, len(p.st.resources)),
		leases:     make(map[string]Lease, len(p.st.leases)),
	}
	for r := range p.st.resources {
		cand.resources[r] = struct{}{}
	}
	for r, l := range p.st.leases {
		cand.leases[r] = l
	}
	touched := map[string]Lease{}
	for _, op := range b.Ops {
		l, leased := cand.leases[op.Resource]
		live := leased && l.ExpiresAt > b.Now
		switch op.Kind {
		case Add:
			if _, ok := cand.resources[op.Resource]; ok {
				return Result{}, ErrExists
			}
			cand.resources[op.Resource] = struct{}{}
			cand.revision++
		case Remove:
			if _, ok := cand.resources[op.Resource]; !ok {
				return Result{}, ErrNotFound
			}
			if leased {
				return Result{}, ErrBusy
			}
			delete(cand.resources, op.Resource)
			cand.revision++
		case Acquire:
			if _, ok := cand.resources[op.Resource]; !ok {
				return Result{}, ErrNotFound
			}
			if live {
				return Result{}, ErrBusy
			}
			cand.revision++
			nl := Lease{Resource: op.Resource, Owner: op.Owner, ExpiresAt: op.ExpiresAt, Revision: cand.revision}
			cand.leases[op.Resource] = nl
			touched[op.Resource] = nl
		case Renew:
			if !leased || !live {
				return Result{}, ErrNotFound
			}
			if l.Owner != op.Owner {
				return Result{}, ErrOwner
			}
			cand.revision++
			nl := Lease{Resource: op.Resource, Owner: op.Owner, ExpiresAt: op.ExpiresAt, Revision: cand.revision}
			cand.leases[op.Resource] = nl
			touched[op.Resource] = nl
		case Release:
			if !leased || !live {
				return Result{}, ErrNotFound
			}
			if l.Owner != op.Owner {
				return Result{}, ErrOwner
			}
			cand.revision++
			delete(cand.leases, op.Resource)
			delete(touched, op.Resource)
		}
	}
	if len(cand.resources) > p.mr {
		return Result{}, ErrCapacity
	}
	// Commit.
	cand.now = b.Now
	if len(b.Ops) > 0 {
		cand.generation++
	}
	p.st = cand
	res := Result{Generation: cand.generation}
	if len(b.Ops) > 0 {
		res.Revision = cand.revision
	}
	keys := make([]string, 0, len(touched))
	for r := range touched {
		keys = append(keys, r)
	}
	sort.Strings(keys)
	for _, r := range keys {
		res.Changed = append(res.Changed, touched[r])
	}
	return res, nil
}

func (p *Pool) Expire(now int64) ([]Lease, error) {
	if now < 0 {
		return nil, ErrInvalidInput
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if now < p.st.now {
		return nil, ErrTime
	}
	var gone []Lease
	for r, l := range p.st.leases {
		if l.ExpiresAt <= now {
			gone = append(gone, l)
			delete(p.st.leases, r)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Resource < gone[j].Resource })
	if len(gone) > 0 {
		p.st.generation++
	}
	p.st.now = now
	return gone, nil
}

func (p *Pool) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Snapshot{
		Generation:   p.st.generation,
		NextRevision: p.st.revision + 1,
		Now:          p.st.now,
		Resources:    make([]string, 0, len(p.st.resources)),
		Leases:       make([]Lease, 0, len(p.st.leases)),
	}
	for r := range p.st.resources {
		s.Resources = append(s.Resources, r)
	}
	sort.Strings(s.Resources)
	for _, r := range s.Resources {
		if l, ok := p.st.leases[r]; ok {
			s.Leases = append(s.Leases, l)
		}
	}
	return s
}
