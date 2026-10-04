package idempotency

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidKey     = errors.New("invalid key")
	ErrInvalidTime    = errors.New("invalid time")
	ErrInvalidToken   = errors.New("invalid token")
	ErrResultTooLarge = errors.New("result too large")
	ErrConflict       = errors.New("fingerprint conflict")
	ErrStaleToken     = errors.New("stale token")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	MaxEntries     int
	MaxResultBytes int
	MaxKeyBytes    int
}

type BeginResult struct {
	Leader     bool
	Pending    bool
	Replay     bool
	Token      uint64
	LeaseUntil int64
	Result     []byte
}

type Record struct {
	Key         string
	Fingerprint string
	Pending     bool
	Token       uint64
	LeaseUntil  int64
	ReplayUntil int64
	Result      []byte
}

type Snapshot struct {
	Generation  uint64
	NextToken   uint64
	Entries     int
	ResultBytes int
	Records     []Record
}

type entry struct {
	fingerprint string
	pending     bool
	token       uint64
	leaseUntil  int64
	replayUntil int64
	result      []byte
}

type Registry struct {
	mu          sync.Mutex
	maxEntries  int
	maxResult   int
	maxKey      int
	entries     map[string]*entry
	generation  uint64
	nextToken   uint64
	resultBytes int
}

func New(opts Options) (*Registry, error) {
	if opts.MaxEntries <= 0 || opts.MaxResultBytes < 0 || opts.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		maxEntries: opts.MaxEntries,
		maxResult:  opts.MaxResultBytes,
		maxKey:     opts.MaxKeyBytes,
		entries:    make(map[string]*entry),
		nextToken:  1,
	}, nil
}

func (r *Registry) checkKey(key, fingerprint string) error {
	if key == "" || len(key) > r.maxKey {
		return ErrInvalidKey
	}
	if fingerprint == "" {
		return ErrInvalidKey
	}
	return nil
}

func (r *Registry) Begin(key, fingerprint string, now, lease int64) (BeginResult, error) {
	if err := r.checkKey(key, fingerprint); err != nil {
		return BeginResult{}, err
	}
	if now < 0 || lease <= 0 {
		return BeginResult{}, ErrInvalidTime
	}
	if now > math.MaxInt64-lease {
		return BeginResult{}, ErrInvalidTime
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if ok {
		if e.pending && now < e.leaseUntil {
			if e.fingerprint == fingerprint {
				return BeginResult{Pending: true, LeaseUntil: e.leaseUntil}, nil
			}
			return BeginResult{}, ErrConflict
		}
		if !e.pending && now < e.replayUntil {
			if e.fingerprint == fingerprint {
				return BeginResult{Replay: true, Result: append([]byte(nil), e.result...)}, nil
			}
			return BeginResult{}, ErrConflict
		}
	}
	if !ok && len(r.entries) >= r.maxEntries {
		return BeginResult{}, ErrCapacity
	}
	if ok && !e.pending {
		r.resultBytes -= len(e.result)
	}
	tok := r.nextToken
	r.nextToken++
	r.entries[key] = &entry{
		fingerprint: fingerprint,
		pending:     true,
		token:       tok,
		leaseUntil:  now + lease,
	}
	r.generation++
	return BeginResult{Leader: true, Token: tok, LeaseUntil: now + lease}, nil
}

func (r *Registry) Renew(key string, token uint64, now, lease int64) error {
	if key == "" || len(key) > r.maxKey {
		return ErrInvalidKey
	}
	if token == 0 {
		return ErrInvalidToken
	}
	if now < 0 || lease <= 0 {
		return ErrInvalidTime
	}
	if now > math.MaxInt64-lease {
		return ErrInvalidTime
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok || !e.pending || e.token != token || now >= e.leaseUntil {
		return ErrStaleToken
	}
	e.leaseUntil = now + lease
	r.generation++
	return nil
}

func (r *Registry) Complete(key string, token uint64, result []byte, now, replayTTL int64) error {
	if key == "" || len(key) > r.maxKey {
		return ErrInvalidKey
	}
	if token == 0 {
		return ErrInvalidToken
	}
	if now < 0 || replayTTL <= 0 {
		return ErrInvalidTime
	}
	if len(result) > r.maxResult {
		return ErrResultTooLarge
	}
	if now > math.MaxInt64-replayTTL {
		return ErrInvalidTime
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok || !e.pending || e.token != token || now >= e.leaseUntil {
		return ErrStaleToken
	}
	cp := append([]byte(nil), result...)
	e.pending = false
	e.result = cp
	e.replayUntil = now + replayTTL
	e.leaseUntil = 0
	r.resultBytes += len(cp)
	r.generation++
	return nil
}

func (r *Registry) Abort(key string, token uint64) error {
	if key == "" || len(key) > r.maxKey {
		return ErrInvalidKey
	}
	if token == 0 {
		return ErrInvalidToken
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok || !e.pending || e.token != token {
		return ErrStaleToken
	}
	delete(r.entries, key)
	r.generation++
	return nil
}

func (r *Registry) Sweep(now int64, limit int) ([]string, error) {
	if now < 0 {
		return nil, ErrInvalidTime
	}
	if limit < 0 {
		return nil, ErrInvalidOptions
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []string
	for k, e := range r.entries {
		if (e.pending && now >= e.leaseUntil) || (!e.pending && now >= e.replayUntil) {
			expired = append(expired, k)
		}
	}
	sort.Strings(expired)
	if limit > 0 && len(expired) > limit {
		expired = expired[:limit]
	}
	if len(expired) == 0 {
		return nil, nil
	}
	for _, k := range expired {
		e := r.entries[k]
		if !e.pending {
			r.resultBytes -= len(e.result)
		}
		delete(r.entries, k)
	}
	r.generation++
	return expired, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		Generation:  r.generation,
		NextToken:   r.nextToken,
		Entries:     len(r.entries),
		ResultBytes: r.resultBytes,
	}
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := r.entries[k]
		rec := Record{Key: k, Fingerprint: e.fingerprint, Pending: e.pending, Token: e.token}
		if e.pending {
			rec.LeaseUntil = e.leaseUntil
		} else {
			rec.ReplayUntil = e.replayUntil
			rec.Result = append([]byte(nil), e.result...)
		}
		s.Records = append(s.Records, rec)
	}
	return s
}
