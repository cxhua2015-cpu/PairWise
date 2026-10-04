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
	used          map[string]map[string]int64 // subject -> dimension -> used
	reservations  map[string]*reservation
	metadataBytes int
	generation    uint64
}

func New(opts Options) (*Manager, error) {
	if opts.MaxSubjects <= 0 || opts.MaxReservations <= 0 || opts.MaxMetadataBytes <= 0 || opts.MaxNameBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Manager{
		opts:         opts,
		limits:       make(map[string]map[string]int64),
		used:         make(map[string]map[string]int64),
		reservations: make(map[string]*reservation),
	}, nil
}

func (m *Manager) validName(s string) bool {
	return len(s) > 0 && len(s) <= m.opts.MaxNameBytes
}

func (m *Manager) SetLimits(limits []Limit) error {
	if len(limits) == 0 {
		return ErrInvalidInput
	}
	type pair struct{ subject, dimension string }
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

	// Candidate subject set for capacity check.
	candidateSubjects := len(m.limits)
	for _, l := range limits {
		if _, ok := m.limits[l.Subject]; !ok {
			candidateSubjects++
		}
	}

	// Semantic check: every current usage must fit the resulting limit.
	for _, l := range limits {
		if m.used[l.Subject][l.Dimension] > l.Amount {
			return ErrExceeded
		}
	}
	if candidateSubjects > m.opts.MaxSubjects {
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

// validateDemands structurally validates demands and returns the canonical
// sorted copy.
func (m *Manager) validateDemands(demands []Demand) ([]Demand, error) {
	if len(demands) == 0 {
		return nil, ErrInvalidInput
	}
	type pair struct{ subject, dimension string }
	seen := make(map[pair]struct{}, len(demands))
	for _, d := range demands {
		if !m.validName(d.Subject) || !m.validName(d.Dimension) || d.Amount <= 0 {
			return nil, ErrInvalidInput
		}
		p := pair{d.Subject, d.Dimension}
		if _, ok := seen[p]; ok {
			return nil, ErrInvalidInput
		}
		seen[p] = struct{}{}
	}
	canonical := make([]Demand, len(demands))
	copy(canonical, demands)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Subject != canonical[j].Subject {
			return canonical[i].Subject < canonical[j].Subject
		}
		return canonical[i].Dimension < canonical[j].Dimension
	})
	return canonical, nil
}

// checkDemands verifies, against the candidate usage (current usage minus any
// demands already removed), that every demand references a configured
// dimension and fits without overflow or exceeding the limit.
func (m *Manager) checkDemands(demands []Demand, removed *reservation) error {
	for _, d := range demands {
		lim, ok := m.limits[d.Subject][d.Dimension]
		if !ok {
			return ErrNotFound
		}
		used := m.used[d.Subject][d.Dimension]
		if removed != nil {
			for _, rd := range removed.demands {
				if rd.Subject == d.Subject && rd.Dimension == d.Dimension {
					used -= rd.Amount
				}
			}
		}
		if d.Amount > lim-used { // lim-used >= 0 always; avoids overflow
			return ErrExceeded
		}
	}
	return nil
}

func (m *Manager) Reserve(id string, demands []Demand, metadata []byte) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	canonical, err := m.validateDemands(demands)
	if err != nil {
		return err
	}
	if len(metadata) > m.opts.MaxMetadataBytes {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.reservations[id]; ok {
		return ErrExists
	}
	if err := m.checkDemands(canonical, nil); err != nil {
		return err
	}
	if len(m.reservations)+1 > m.opts.MaxReservations {
		return ErrCapacity
	}
	if m.metadataBytes+len(metadata) > m.opts.MaxMetadataBytes {
		return ErrCapacity
	}

	m.applyDemands(canonical, nil)
	var metaCopy []byte
	if metadata != nil {
		metaCopy = make([]byte, len(metadata))
		copy(metaCopy, metadata)
	}
	m.reservations[id] = &reservation{demands: canonical, metadata: metaCopy}
	m.metadataBytes += len(metadata)
	m.generation++
	return nil
}

func (m *Manager) Replace(id string, demands []Demand, metadata []byte) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	canonical, err := m.validateDemands(demands)
	if err != nil {
		return err
	}
	if len(metadata) > m.opts.MaxMetadataBytes {
		return ErrInvalidInput
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	old, ok := m.reservations[id]
	if !ok {
		return ErrNotFound
	}
	if err := m.checkDemands(canonical, old); err != nil {
		return err
	}
	if m.metadataBytes-len(old.metadata)+len(metadata) > m.opts.MaxMetadataBytes {
		return ErrCapacity
	}

	m.applyDemands(canonical, old)
	var metaCopy []byte
	if metadata != nil {
		metaCopy = make([]byte, len(metadata))
		copy(metaCopy, metadata)
	}
	m.metadataBytes += len(metadata) - len(old.metadata)
	m.reservations[id] = &reservation{demands: canonical, metadata: metaCopy}
	m.generation++
	return nil
}

// applyDemands subtracts old demands and adds new demands to usage.
func (m *Manager) applyDemands(add []Demand, removed *reservation) {
	if removed != nil {
		for _, d := range removed.demands {
			m.used[d.Subject][d.Dimension] -= d.Amount
		}
	}
	for _, d := range add {
		dims, ok := m.used[d.Subject]
		if !ok {
			dims = make(map[string]int64)
			m.used[d.Subject] = dims
		}
		dims[d.Dimension] += d.Amount
	}
}

func (m *Manager) Release(id string) error {
	if !m.validName(id) {
		return ErrInvalidInput
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.reservations[id]
	if !ok {
		return ErrNotFound
	}
	m.applyDemands(nil, r)
	m.metadataBytes -= len(r.metadata)
	delete(m.reservations, id)
	m.generation++
	return nil
}

func (m *Manager) DeleteSubject(subject string) error {
	if !m.validName(subject) {
		return ErrInvalidInput
	}
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
	delete(m.used, subject)
	m.generation++
	return nil
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := Snapshot{
		Generation:    m.generation,
		Subjects:      len(m.limits),
		Reservations:  len(m.reservations),
		MetadataBytes: m.metadataBytes,
	}
	subjects := make([]string, 0, len(m.limits))
	for name := range m.limits {
		subjects = append(subjects, name)
	}
	sort.Strings(subjects)
	for _, name := range subjects {
		dims := m.limits[name]
		names := make([]string, 0, len(dims))
		for d := range dims {
			names = append(names, d)
		}
		sort.Strings(names)
		ss := SubjectSnapshot{Subject: name, Dimensions: make([]DimensionSnapshot, 0, len(names))}
		for _, d := range names {
			ss.Dimensions = append(ss.Dimensions, DimensionSnapshot{
				Dimension: d,
				Limit:     dims[d],
				Used:      m.used[name][d],
			})
		}
		s.SubjectState = append(s.SubjectState, ss)
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
		s.ReservationState = append(s.ReservationState, ReservationSnapshot{
			ID:       id,
			Demands:  demands,
			Metadata: meta,
		})
	}
	return s
}
