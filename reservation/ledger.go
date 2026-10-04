package reservation

import (
	"fmt"
	"sort"
	"sync"
)

// entry is one stored reservation. Values are owned exclusively by the ledger.
type entry struct {
	id       string
	resource string
	start    int64
	end      int64
	value    []byte
}

// less orders entries by (start, end, id); half-open disjoint intervals.
func (e *entry) less(o *entry) bool {
	if e.start != o.start {
		return e.start < o.start
	}
	if e.end != o.end {
		return e.end < o.end
	}
	return e.id < o.id
}

func (e *entry) toReservation() Reservation {
	return Reservation{
		ID:       e.id,
		Resource: e.resource,
		Start:    e.start,
		End:      e.end,
		Value:    cloneBytes(e.value),
	}
}

type resourceState struct {
	name    string
	entries []*entry // sorted by (start, end, id); disjoint half-open intervals
}

// find returns the insertion/locate index for key and whether an exact
// (start, end, id) match exists.
func (rs *resourceState) find(key *entry) (int, bool) {
	i := sort.Search(len(rs.entries), func(i int) bool {
		return !rs.entries[i].less(key)
	})
	if i < len(rs.entries) && rs.entries[i].id == key.id &&
		rs.entries[i].start == key.start && rs.entries[i].end == key.end {
		return i, true
	}
	return i, false
}

func (rs *resourceState) insert(e *entry) {
	i, _ := rs.find(e)
	rs.entries = append(rs.entries, nil)
	copy(rs.entries[i+1:], rs.entries[i:])
	rs.entries[i] = e
}

func (rs *resourceState) remove(e *entry) {
	i, ok := rs.find(e)
	if !ok {
		return
	}
	rs.entries = append(rs.entries[:i], rs.entries[i+1:]...)
}

// overlaps reports whether [start,end) intersects any stored interval.
func (rs *resourceState) overlaps(start, end int64) bool {
	i := sort.Search(len(rs.entries), func(i int) bool {
		return rs.entries[i].end > start
	})
	return i < len(rs.entries) && rs.entries[i].start < end
}

type state struct {
	resources map[string]*resourceState
	byID      map[string]*entry
	resCount  int
	valBytes  int
}

func newState() *state {
	return &state{resources: map[string]*resourceState{}, byID: map[string]*entry{}}
}

// clone builds an isolated deep copy used as the transaction candidate.
func (s *state) clone() *state {
	c := &state{
		resources: make(map[string]*resourceState, len(s.resources)),
		byID:      make(map[string]*entry, len(s.byID)),
		resCount:  s.resCount,
		valBytes:  s.valBytes,
	}
	for name, rs := range s.resources {
		nrs := &resourceState{name: name, entries: make([]*entry, len(rs.entries))}
		for i, e := range rs.entries {
			ne := &entry{
				id:       e.id,
				resource: e.resource,
				start:    e.start,
				end:      e.end,
				value:    cloneBytes(e.value),
			}
			nrs.entries[i] = ne
			c.byID[ne.id] = ne
		}
		c.resources[name] = nrs
	}
	return c
}

func (s *state) add(r Reservation) {
	e := &entry{
		id:       r.ID,
		resource: r.Resource,
		start:    r.Start,
		end:      r.End,
		value:    cloneBytes(r.Value),
	}
	rs := s.resources[r.Resource]
	if rs == nil {
		rs = &resourceState{name: r.Resource}
		s.resources[r.Resource] = rs
	}
	rs.insert(e)
	s.byID[e.id] = e
	s.resCount++
	s.valBytes += len(e.value)
}

func (s *state) remove(id string) {
	e := s.byID[id]
	if e == nil {
		return
	}
	rs := s.resources[e.resource]
	rs.remove(e)
	if len(rs.entries) == 0 {
		delete(s.resources, rs.name)
	}
	delete(s.byID, id)
	s.resCount--
	s.valBytes -= len(e.value)
}

// Ledger is a concurrency-safe in-memory reservation store.
type Ledger struct {
	mu         sync.RWMutex
	opts       Options
	state      *state
	generation uint64
}

func New(opts Options) (*Ledger, error) {
	if opts.MaxResources < 1 || opts.MaxResources > 10000 ||
		opts.MaxReservations < 1 || opts.MaxReservations > 1_000_000 ||
		opts.MaxValueBytes < 1 || opts.MaxValueBytes > 64<<20 {
		return nil, ErrInvalidOptions
	}
	return &Ledger{opts: opts, state: newState()}, nil
}

func (l *Ledger) Apply(c Change) (uint64, error) {
	return l.ApplyBatch([]Change{c})
}

// validateAll performs every structural check in input order before any
// semantic effect.
func validateAll(changes []Change) error {
	for i := range changes {
		c := &changes[i]
		switch c.Type {
		case ChangeAdd:
			if c.ID != "" {
				return fmt.Errorf("%w: add: ID field must be empty", ErrInvalidChange)
			}
			r := c.Reservation
			if !validName(r.ID) {
				return fmt.Errorf("%w: add: invalid reservation id %q", ErrInvalidID, r.ID)
			}
			if !validName(r.Resource) {
				return fmt.Errorf("%w: add: invalid resource %q", ErrInvalidResource, r.Resource)
			}
			if r.Start >= r.End {
				return fmt.Errorf("%w: add: start must be before end", ErrInvalidInterval)
			}
			if len(r.Value) > maxSingleValue {
				return fmt.Errorf("%w: add: value exceeds 1 MiB", ErrValueTooLarge)
			}
		case ChangeDelete:
			if c.Reservation.ID != "" {
				return fmt.Errorf("%w: delete: Reservation.ID must be empty", ErrInvalidChange)
			}
			if !validName(c.ID) {
				return fmt.Errorf("%w: delete: invalid id %q", ErrInvalidID, c.ID)
			}
		default:
			return fmt.Errorf("%w: unknown change type %d", ErrInvalidChange, c.Type)
		}
	}
	return nil
}

func (l *Ledger) ApplyBatch(changes []Change) (uint64, error) {
	if err := validateAll(changes); err != nil {
		l.mu.RLock()
		g := l.generation
		l.mu.RUnlock()
		return g, err
	}
	if len(changes) == 0 {
		l.mu.RLock()
		g := l.generation
		l.mu.RUnlock()
		return g, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cand := l.state.clone()

	for i := range changes {
		c := &changes[i]
		switch c.Type {
		case ChangeAdd:
			r := c.Reservation
			if _, ok := cand.byID[r.ID]; ok {
				return l.generation, fmt.Errorf("%w: id %q", ErrDuplicate, r.ID)
			}
			cand.add(r)
		case ChangeDelete:
			if _, ok := cand.byID[c.ID]; !ok {
				return l.generation, fmt.Errorf("%w: id %q", ErrNotFound, c.ID)
			}
			cand.remove(c.ID)
		}
	}

	if len(cand.resources) > l.opts.MaxResources ||
		cand.resCount > l.opts.MaxReservations ||
		cand.valBytes > l.opts.MaxValueBytes {
		return l.generation, fmt.Errorf("%w: final state exceeds configured capacity", ErrCapacity)
	}

	// Final-state overlap check on every resource; entries are sorted, so a
	// linear adjacent scan suffices. Touching half-open intervals are fine.
	for _, rs := range cand.resources {
		for i := 1; i < len(rs.entries); i++ {
			prev, cur := rs.entries[i-1], rs.entries[i]
			if cur.start < prev.end {
				return l.generation, fmt.Errorf("%w: overlapping intervals on resource %q", ErrConflict, rs.name)
			}
		}
	}

	l.state = cand
	l.generation++
	return l.generation, nil
}

// At returns the reservation covering time on resource, if any.
func (l *Ledger) At(resource string, time int64) (AtResult, error) {
	if !validName(resource) {
		return AtResult{}, fmt.Errorf("%w: %q", ErrInvalidResource, resource)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := AtResult{Generation: l.generation}
	rs := l.state.resources[resource]
	if rs == nil {
		return res, nil
	}
	// First entry with end > time; covering iff its start <= time.
	i := sort.Search(len(rs.entries), func(i int) bool {
		return rs.entries[i].end > time
	})
	if i < len(rs.entries) && rs.entries[i].start <= time {
		res.Found = true
		res.Reservation = rs.entries[i].toReservation()
	}
	return res, nil
}

// Scan returns reservations overlapping [from,to), sorted by (Start,End,ID),
// capped at limit.
func (l *Ledger) Scan(resource string, from, to int64, limit int) (ScanResult, error) {
	if !validName(resource) {
		return ScanResult{}, fmt.Errorf("%w: %q", ErrInvalidResource, resource)
	}
	if from >= to || limit < 1 || limit > maxScanLimit {
		return ScanResult{}, fmt.Errorf("%w: from=%d to=%d limit=%d", ErrInvalidScan, from, to, limit)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	res := ScanResult{Generation: l.generation}
	rs := l.state.resources[resource]
	if rs == nil {
		return res, nil
	}
	i := sort.Search(len(rs.entries), func(i int) bool {
		return rs.entries[i].end > from
	})
	for ; i < len(rs.entries) && len(res.Reservations) < limit; i++ {
		e := rs.entries[i]
		if e.start >= to {
			break
		}
		res.Reservations = append(res.Reservations, e.toReservation())
	}
	return res, nil
}

// Snapshot returns a deep, consistently ordered copy of the ledger.
func (l *Ledger) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s := Snapshot{
		Generation:       l.generation,
		UsedReservations: l.state.resCount,
		UsedValueBytes:   l.state.valBytes,
	}
	names := make([]string, 0, len(l.state.resources))
	for name := range l.state.resources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rs := l.state.resources[name]
		rv := ResourceView{Resource: name, Reservations: make([]Reservation, len(rs.entries))}
		for i, e := range rs.entries {
			rv.Reservations[i] = e.toReservation()
		}
		s.Resources = append(s.Resources, rv)
	}
	return s
}
