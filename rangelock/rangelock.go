package rangelock

import (
	"errors"
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

type lockRecord struct {
	owner, resource string
	start, end      int64
	mode            Mode
	token           uint64
	expiresAt       int64
	metadata        []byte
}

type Manager struct {
	mu         sync.Mutex
	opts       Options
	locks      map[string]*lockRecord
	nextToken  uint64
	generation uint64
}

func New(opts Options) (*Manager, error) {
	if opts.MaxLocks <= 0 || opts.MaxOwners <= 0 || opts.MaxMetadataBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Manager{opts: opts, locks: make(map[string]*lockRecord), nextToken: 1}, nil
}

const maxInt64 = int64(^uint64(0) >> 1)

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
	if r.TTL > maxInt64-now {
		return ErrInvalidTime
	}
	return nil
}

func overlaps(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart < bEnd && bStart < aEnd
}

func cloneRecord(r *lockRecord) *lockRecord {
	c := *r
	if r.metadata != nil {
		c.metadata = append([]byte(nil), r.metadata...)
	}
	return &c
}

func (m *Manager) AcquireBatch(now int64, requests []Request) ([]Lease, uint64, error) {
	if now < 0 {
		return nil, 0, ErrInvalidTime
	}
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

	candidate := make(map[string]*lockRecord, len(m.locks)+len(requests))
	for id, rec := range m.locks {
		if now < rec.expiresAt {
			candidate[id] = cloneRecord(rec)
		}
	}
	nextToken := m.nextToken

	type accepted struct {
		id  string
		rec *lockRecord
	}
	acceptedList := make([]accepted, 0, len(requests))

	for i := range requests {
		r := &requests[i]
		if _, exists := candidate[r.ID]; exists {
			return nil, 0, ErrExists
		}
		for _, rec := range candidate {
			if rec.resource != r.Resource {
				continue
			}
			if !overlaps(rec.start, rec.end, r.Start, r.End) {
				continue
			}
			if rec.mode == Write || r.Mode == Write {
				return nil, 0, ErrConflict
			}
		}
		rec := &lockRecord{
			owner:     r.Owner,
			resource:  r.Resource,
			start:     r.Start,
			end:       r.End,
			mode:      r.Mode,
			token:     nextToken,
			expiresAt: now + r.TTL,
		}
		if r.Metadata != nil {
			rec.metadata = append([]byte(nil), r.Metadata...)
		}
		nextToken++
		candidate[r.ID] = rec
		acceptedList = append(acceptedList, accepted{r.ID, rec})
	}

	if len(candidate) > m.opts.MaxLocks {
		return nil, 0, ErrCapacity
	}
	owners := make(map[string]struct{}, len(candidate))
	metaBytes := 0
	for _, rec := range candidate {
		owners[rec.owner] = struct{}{}
		metaBytes += len(rec.metadata)
	}
	if len(owners) > m.opts.MaxOwners || metaBytes > m.opts.MaxMetadataBytes {
		return nil, 0, ErrCapacity
	}

	m.locks = candidate
	m.nextToken = nextToken
	m.generation++

	leases := make([]Lease, 0, len(acceptedList))
	for _, a := range acceptedList {
		rec := a.rec
		leases = append(leases, Lease{
			ID:        a.id,
			Owner:     rec.owner,
			Resource:  rec.resource,
			Start:     rec.start,
			End:       rec.end,
			Mode:      rec.mode,
			Token:     rec.token,
			ExpiresAt: rec.expiresAt,
			Metadata:  append([]byte(nil), rec.metadata...),
		})
	}
	return leases, m.generation, nil
}

func (m *Manager) Renew(id string, token uint64, now, ttl int64) error {
	if now < 0 {
		return ErrInvalidTime
	}
	if ttl <= 0 || !m.validName(id) {
		return ErrInvalidInput
	}
	if ttl > maxInt64-now {
		return ErrInvalidTime
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.locks[id]
	if !ok || rec.token != token || now >= rec.expiresAt {
		return ErrStaleToken
	}
	rec.expiresAt = now + ttl
	m.generation++
	return nil
}

func (m *Manager) Release(id string, token uint64) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.locks[id]
	if !ok || rec.token != token {
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
	for id, rec := range m.locks {
		if now >= rec.expiresAt {
			expired = append(expired, id)
		}
	}
	sort.Strings(expired)
	if limit > 0 && len(expired) > limit {
		expired = expired[:limit]
	}
	if len(expired) == 0 {
		return nil, nil
	}
	for _, id := range expired {
		delete(m.locks, id)
	}
	m.generation++
	return expired, nil
}

func (m *Manager) Query(resource string, start, end, now int64) ([]Lease, error) {
	if now < 0 {
		return nil, ErrInvalidTime
	}
	if !m.validName(resource) || start >= end {
		return nil, ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Lease
	for id, rec := range m.locks {
		if rec.resource != resource || now >= rec.expiresAt {
			continue
		}
		if !overlaps(rec.start, rec.end, start, end) {
			continue
		}
		out = append(out, leaseOf(id, rec))
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
	return out, nil
}

func leaseOf(id string, rec *lockRecord) Lease {
	return Lease{
		ID:        id,
		Owner:     rec.owner,
		Resource:  rec.resource,
		Start:     rec.start,
		End:       rec.end,
		Mode:      rec.mode,
		Token:     rec.token,
		ExpiresAt: rec.expiresAt,
		Metadata:  append([]byte(nil), rec.metadata...),
	}
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{
		Generation: m.generation,
		NextToken:  m.nextToken,
		Locks:      len(m.locks),
	}
	owners := make(map[string]struct{}, len(m.locks))
	for id, rec := range m.locks {
		owners[rec.owner] = struct{}{}
		s.MetadataBytes += len(rec.metadata)
		s.Leases = append(s.Leases, leaseOf(id, rec))
	}
	s.Owners = len(owners)
	sort.Slice(s.Leases, func(i, j int) bool {
		if s.Leases[i].Resource != s.Leases[j].Resource {
			return s.Leases[i].Resource < s.Leases[j].Resource
		}
		if s.Leases[i].Start != s.Leases[j].Start {
			return s.Leases[i].Start < s.Leases[j].Start
		}
		if s.Leases[i].End != s.Leases[j].End {
			return s.Leases[i].End < s.Leases[j].End
		}
		return s.Leases[i].ID < s.Leases[j].ID
	})
	return s
}
