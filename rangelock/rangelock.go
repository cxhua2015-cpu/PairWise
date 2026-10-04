package rangelock

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrInvalidTime    = errors.New("invalid time")
	ErrInvalidToken   = errors.New("invalid token")
	ErrExists         = errors.New("id exists")
	ErrConflict       = errors.New("lock conflict")
	ErrCapacity       = errors.New("capacity exceeded")
	ErrStaleToken     = errors.New("stale token")
)

type Mode uint8

const (
	Read Mode = iota + 1
	Write
)

type Options struct{ MaxLocks, MaxOwners, MaxMetadataBytes, MaxNameBytes int }
type Request struct {
	ID, Owner, Resource string
	Start, End          int64
	Mode                Mode
	TTL                 int64
	Metadata            []byte
}
type Lease struct {
	ID, Owner, Resource string
	Start, End          int64
	Mode                Mode
	Token               uint64
	ExpiresAt           int64
	Metadata            []byte
}
type Snapshot struct {
	Generation, NextToken        uint64
	Locks, Owners, MetadataBytes int
	Leases                       []Lease
}

type lockEntry struct {
	Lease
}

type Manager struct {
	mu         sync.Mutex
	opts       Options
	locks      map[string]*lockEntry // by ID
	generation uint64
	nextToken  uint64
}

func New(opts Options) (*Manager, error) {
	if opts.MaxLocks <= 0 || opts.MaxOwners <= 0 || opts.MaxMetadataBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Manager{opts: opts, locks: make(map[string]*lockEntry), nextToken: 1}, nil
}

func (m *Manager) validName(s string) bool {
	return len(s) > 0 && len(s) <= m.opts.MaxNameBytes
}

func (m *Manager) validateRequest(now int64, r *Request) error {
	if !m.validName(r.ID) || !m.validName(r.Owner) || !m.validName(r.Resource) {
		return ErrInvalidInput
	}
	if r.Start >= r.End {
		return ErrInvalidInput
	}
	if r.Mode != Read && r.Mode != Write {
		return ErrInvalidInput
	}
	if r.TTL <= 0 {
		return ErrInvalidInput
	}
	if len(r.Metadata) > m.opts.MaxMetadataBytes {
		return ErrInvalidInput
	}
	if now > math.MaxInt64-r.TTL {
		return ErrInvalidTime
	}
	return nil
}

func overlaps(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart < bEnd && bStart < aEnd
}

func conflicts(a, b *Lease) bool {
	if a.Resource != b.Resource {
		return false
	}
	if !overlaps(a.Start, a.End, b.Start, b.End) {
		return false
	}
	return a.Mode == Write || b.Mode == Write
}

func (m *Manager) AcquireBatch(now int64, requests []Request) ([]Lease, uint64, error) {
	if now < 0 {
		return nil, 0, ErrInvalidTime
	}
	// Full structural validation before any state lookup.
	seen := make(map[string]struct{}, len(requests))
	for i := range requests {
		r := &requests[i]
		if err := m.validateRequest(now, r); err != nil {
			return nil, 0, err
		}
		if _, dup := seen[r.ID]; dup {
			return nil, 0, ErrInvalidInput
		}
		seen[r.ID] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(requests) == 0 {
		return nil, m.generation, nil
	}

	// Isolated candidate state.
	candidate := make(map[string]*lockEntry, len(m.locks)+len(requests))
	for id, e := range m.locks {
		if now >= e.ExpiresAt {
			continue // expiry pruning inside the candidate
		}
		candidate[id] = e
	}
	token := m.nextToken
	accepted := make([]*lockEntry, 0, len(requests))
	for i := range requests {
		r := &requests[i]
		if _, ok := candidate[r.ID]; ok {
			return nil, 0, ErrExists
		}
		lease := Lease{
			ID: r.ID, Owner: r.Owner, Resource: r.Resource,
			Start: r.Start, End: r.End, Mode: r.Mode,
			Token: token, ExpiresAt: now + r.TTL, Metadata: r.Metadata,
		}
		for _, e := range candidate {
			if conflicts(&lease, &e.Lease) {
				return nil, 0, ErrConflict
			}
		}
		entry := &lockEntry{Lease: lease}
		candidate[r.ID] = entry
		accepted = append(accepted, entry)
		token++
	}

	// Capacity checks only after the whole batch is applied.
	if len(candidate) > m.opts.MaxLocks {
		return nil, 0, ErrCapacity
	}
	owners := make(map[string]struct{})
	metaBytes := 0
	for _, e := range candidate {
		owners[e.Owner] = struct{}{}
		metaBytes += len(e.Metadata)
	}
	if len(owners) > m.opts.MaxOwners || metaBytes > m.opts.MaxMetadataBytes {
		return nil, 0, ErrCapacity
	}

	// Commit: deep-copy metadata on the way in and install the candidate.
	for _, e := range accepted {
		e.Metadata = copyBytes(e.Metadata)
	}
	m.locks = candidate
	m.nextToken = token
	m.generation++

	out := make([]Lease, len(accepted))
	for i, e := range accepted {
		out[i] = e.Lease
		out[i].Metadata = copyBytes(e.Metadata)
	}
	return out, m.generation, nil
}

func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func (m *Manager) Renew(id string, token uint64, now, ttl int64) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	if ttl <= 0 {
		return ErrInvalidInput
	}
	if now < 0 {
		return ErrInvalidTime
	}
	if now > math.MaxInt64-ttl {
		return ErrInvalidTime
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.locks[id]
	if !ok || e.Token != token || now >= e.ExpiresAt {
		return ErrStaleToken
	}
	e.ExpiresAt = now + ttl
	m.generation++
	return nil
}

func (m *Manager) Release(id string, token uint64) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.locks[id]
	if !ok || e.Token != token {
		return ErrStaleToken
	}
	delete(m.locks, id)
	m.generation++
	return nil
}

func (m *Manager) Sweep(now int64, limit int) ([]string, error) {
	if now < 0 {
		return nil, ErrInvalidTime
	}
	if limit < 0 {
		return nil, ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var expired []string
	for id, e := range m.locks {
		if now >= e.ExpiresAt {
			expired = append(expired, id)
		}
	}
	sort.Strings(expired)
	if limit > 0 && len(expired) > limit {
		expired = expired[:limit]
	}
	for _, id := range expired {
		delete(m.locks, id)
	}
	if len(expired) > 0 {
		m.generation++
	}
	return expired, nil
}

func (m *Manager) Query(resource string, start, end, now int64) ([]Lease, error) {
	if !m.validName(resource) {
		return nil, ErrInvalidInput
	}
	if start >= end {
		return nil, ErrInvalidInput
	}
	if now < 0 {
		return nil, ErrInvalidTime
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Lease
	for _, e := range m.locks {
		if e.Resource != resource || now >= e.ExpiresAt {
			continue
		}
		if overlaps(e.Start, e.End, start, end) {
			out = append(out, e.Lease)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		if out[i].End != out[j].End {
			return out[i].End < out[j].End
		}
		return out[i].ID < out[j].ID
	})
	for i := range out {
		out[i].Metadata = copyBytes(out[i].Metadata)
	}
	return out, nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Generation: m.generation, NextToken: m.nextToken}
	owners := make(map[string]struct{})
	for _, e := range m.locks {
		s.Leases = append(s.Leases, e.Lease)
		owners[e.Owner] = struct{}{}
		s.MetadataBytes += len(e.Metadata)
	}
	sort.Slice(s.Leases, func(i, j int) bool {
		a, b := s.Leases[i], s.Leases[j]
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		return a.ID < b.ID
	})
	for i := range s.Leases {
		s.Leases[i].Metadata = copyBytes(s.Leases[i].Metadata)
	}
	s.Locks = len(s.Leases)
	s.Owners = len(owners)
	return s
}
