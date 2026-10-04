package quota

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrExists         = errors.New("already exists")
	ErrExceeded       = errors.New("quota exceeded")
	ErrCapacity       = errors.New("capacity exceeded")
	ErrBusy           = errors.New("subject busy")
)

type Options struct{ MaxSubjects, MaxReservations, MaxMetadataBytes, MaxNameBytes int }
type Limit struct {
	Subject, Dimension string
	Amount             int64
}
type Demand struct {
	Subject, Dimension string
	Amount             int64
}
type DimensionSnapshot struct {
	Dimension   string
	Limit, Used int64
}
type SubjectSnapshot struct {
	Subject    string
	Dimensions []DimensionSnapshot
}
type ReservationSnapshot struct {
	ID       string
	Demands  []Demand
	Metadata []byte
}
type Snapshot struct {
	Generation                            uint64
	Subjects, Reservations, MetadataBytes int
	SubjectState                          []SubjectSnapshot
	ReservationState                      []ReservationSnapshot
}

type reservation struct {
	demands  []Demand // canonical: sorted by subject then dimension
	metadata []byte
}

type Manager struct {
	mu            sync.Mutex
	opts          Options
	limits        map[string]map[string]int64 // subject -> dimension -> limit
	usage         map[string]map[string]int64 // subject -> dimension -> used
	reservations  map[string]*reservation
	metadataBytes int
	generation    uint64
}

func New(opts Options) (*Manager, error) {
	if opts.MaxSubjects <= 0 || opts.MaxReservations <= 0 ||
		opts.MaxMetadataBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Manager{
		opts:         opts,
		limits:       make(map[string]map[string]int64),
		usage:        make(map[string]map[string]int64),
		reservations: make(map[string]*reservation),
	}, nil
}

func (m *Manager) validName(s string) bool {
	return len(s) > 0 && len(s) <= m.opts.MaxNameBytes
}

// validateDemands fully structurally validates a demand list.
func (m *Manager) validateDemands(demands []Demand) error {
	if len(demands) == 0 {
		return ErrInvalidInput
	}
	type pair struct{ s, d string }
	seen := make(map[pair]struct{}, len(demands))
	for _, d := range demands {
		if !m.validName(d.Subject) || !m.validName(d.Dimension) || d.Amount <= 0 {
			return ErrInvalidInput
		}
		p := pair{d.Subject, d.Dimension}
		if _, ok := seen[p]; ok {
			return ErrInvalidInput
		}
		seen[p] = struct{}{}
	}
	return nil
}

func canonicalDemands(demands []Demand) []Demand {
	out := make([]Demand, len(demands))
	copy(out, demands)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Dimension < out[j].Dimension
	})
	return out
}

func (m *Manager) usedAt(subject, dimension string) int64 {
	if dims, ok := m.usage[subject]; ok {
		return dims[dimension]
	}
	return 0
}

func (m *Manager) addUsage(subject, dimension string, delta int64) {
	dims, ok := m.usage[subject]
	if !ok {
		dims = make(map[string]int64)
		m.usage[subject] = dims
	}
	dims[dimension] += delta
	if dims[dimension] == 0 {
		delete(dims, dimension)
	}
	if len(dims) == 0 {
		delete(m.usage, subject)
	}
}

// checkFits verifies that adding demands to current usage stays within
// configured limits without int64 overflow. Demands are pre-sorted so each
// (subject, dimension) appears at most once.
func (m *Manager) checkFits(demands []Demand) error {
	for _, d := range demands {
		lim, ok := m.limits[d.Subject][d.Dimension]
		if !ok {
			return ErrNotFound
		}
		used := m.usedAt(d.Subject, d.Dimension)
		if d.Amount > lim-used {
			return ErrExceeded
		}
	}
	return nil
}

func (m *Manager) applyDemands(demands []Demand, sign int64) {
	for _, d := range demands {
		m.addUsage(d.Subject, d.Dimension, sign*d.Amount)
	}
}

func (m *Manager) SetLimits(limits []Limit) error {
	// Full structural validation before any state lookup.
	if len(limits) == 0 {
		return ErrInvalidInput
	}
	type pair struct{ s, d string }
	seen := make(map[pair]struct{}, len(limits))
	for _, l := range limits {
		if !m.validName(l.Subject) || !m.validName(l.Dimension) || l.Amount < 0 {
			return ErrInvalidInput
		}
		p := pair{l.Subject, l.Dimension}
		if _, ok := seen[p]; ok {
			return ErrInvalidInput
		}
		seen[p] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Semantic checks against an isolated candidate: every currently used
	// amount must fit the resulting limit.
	newSubjects := 0
	for _, l := range limits {
		if _, ok := m.limits[l.Subject]; !ok {
			newSubjects++
		}
		if m.usedAt(l.Subject, l.Dimension) > l.Amount {
			return ErrExceeded
		}
	}
	// Capacity check last.
	if len(m.limits)+newSubjects > m.opts.MaxSubjects {
		return ErrCapacity
	}

	for _, l := range limits {
		dims, ok := m.limits[l.Subject]
		if !ok {
			dims = make(map[string]int64)
			m.limits[l.Subject] = dims
		}
		dims[l.Dimension] = l.Amount
	}
	m.generation++
	return nil
}

func (m *Manager) Reserve(id string, demands []Demand, metadata []byte) error {
	// Structural validation first.
	if !m.validName(id) || len(metadata) > m.opts.MaxMetadataBytes {
		return ErrInvalidInput
	}
	if err := m.validateDemands(demands); err != nil {
		return err
	}
	canon := canonicalDemands(demands)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.reservations[id]; ok {
		return ErrExists
	}
	if err := m.checkFits(canon); err != nil {
		return err
	}
	// Global capacity checks only after all semantic checks.
	if len(m.reservations)+1 > m.opts.MaxReservations {
		return ErrCapacity
	}
	if m.metadataBytes+len(metadata) > m.opts.MaxMetadataBytes {
		return ErrCapacity
	}

	m.applyDemands(canon, 1)
	var metaCopy []byte
	if metadata != nil {
		metaCopy = make([]byte, len(metadata))
		copy(metaCopy, metadata)
	}
	m.reservations[id] = &reservation{demands: canon, metadata: metaCopy}
	m.metadataBytes += len(metadata)
	m.generation++
	return nil
}

func (m *Manager) Replace(id string, demands []Demand, metadata []byte) error {
	if !m.validName(id) || len(metadata) > m.opts.MaxMetadataBytes {
		return ErrInvalidInput
	}
	if err := m.validateDemands(demands); err != nil {
		return err
	}
	canon := canonicalDemands(demands)

	m.mu.Lock()
	defer m.mu.Unlock()

	old, ok := m.reservations[id]
	if !ok {
		return ErrNotFound
	}
	// Atomically remove old demands before validating the replacement so
	// capacity can move between dimensions.
	m.applyDemands(old.demands, -1)
	if err := m.checkFits(canon); err != nil {
		m.applyDemands(old.demands, 1) // roll back
		return err
	}
	// Metadata capacity check after semantic checks; count is unchanged.
	if m.metadataBytes-len(old.metadata)+len(metadata) > m.opts.MaxMetadataBytes {
		m.applyDemands(old.demands, 1) // roll back
		return ErrCapacity
	}

	m.applyDemands(canon, 1)
	var metaCopy []byte
	if metadata != nil {
		metaCopy = make([]byte, len(metadata))
		copy(metaCopy, metadata)
	}
	m.metadataBytes += len(metadata) - len(old.metadata)
	m.reservations[id] = &reservation{demands: canon, metadata: metaCopy}
	m.generation++
	return nil
}

func (m *Manager) Release(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.reservations[id]
	if !ok {
		return ErrNotFound
	}
	m.applyDemands(r.demands, -1)
	m.metadataBytes -= len(r.metadata)
	delete(m.reservations, id)
	m.generation++
	return nil
}

func (m *Manager) DeleteSubject(subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.limits[subject]; !ok {
		return ErrNotFound
	}
	for _, r := range m.reservations {
		for _, d := range r.demands {
			if d.Subject == subject {
				return ErrBusy
			}
		}
	}
	delete(m.limits, subject)
	delete(m.usage, subject)
	m.generation++
	return nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := Snapshot{
		Generation:   m.generation,
		Subjects:     len(m.limits),
		Reservations: len(m.reservations),
		// MetadataBytes set below.
	}
	snap.MetadataBytes = m.metadataBytes

	subjects := make([]string, 0, len(m.limits))
	for s := range m.limits {
		subjects = append(subjects, s)
	}
	sort.Strings(subjects)
	for _, s := range subjects {
		dims := m.limits[s]
		names := make([]string, 0, len(dims))
		for d := range dims {
			names = append(names, d)
		}
		sort.Strings(names)
		ss := SubjectSnapshot{Subject: s, Dimensions: make([]DimensionSnapshot, 0, len(names))}
		for _, d := range names {
			ss.Dimensions = append(ss.Dimensions, DimensionSnapshot{
				Dimension: d,
				Limit:     dims[d],
				Used:      m.usedAt(s, d),
			})
		}
		snap.SubjectState = append(snap.SubjectState, ss)
	}

	ids := make([]string, 0, len(m.reservations))
	for id := range m.reservations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := m.reservations[id]
		demands := make([]Demand, len(r.demands))
		copy(demands, r.demands)
		var meta []byte
		if r.metadata != nil {
			meta = make([]byte, len(r.metadata))
			copy(meta, r.metadata)
		}
		snap.ReservationState = append(snap.ReservationState, ReservationSnapshot{
			ID:       id,
			Demands:  demands,
			Metadata: meta,
		})
	}
	return snap
}

