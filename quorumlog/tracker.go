package quorumlog

import (
	"bytes"
	"sort"
	"sync"
)

const (
	maxIdentLen = 128
	maxTime     = 1_000_000_000_000
	maxIndex    = 1_000_000_000_000
)

func validIdent(s string) bool { return len(s) > 0 && len(s) <= maxIdentLen }
func validTime(at int64) bool  { return at >= 0 && at <= maxTime }
func validIndex(i uint64) bool { return i >= 1 && i <= maxIndex }

// pendingEntry is immutable once stored.
type pendingEntry struct {
	digest  string
	payload []byte
}

type stream struct {
	id               string
	voters           []string // sorted lexicographically
	voterSet         map[string]struct{}
	quorum           int
	committed        uint64
	committedDigests map[uint64]string
	sealed           bool
	lastIndex        uint64
	lastActivity     int64
	completed        bool
	highestObserved  uint64
	entries          map[uint64]*pendingEntry
	acks             map[uint64]map[string]string
	pendingAcks      int
	bufferedBytes    int
}

func (s *stream) clone() *stream {
	c := &stream{
		id:               s.id,
		voters:           append([]string(nil), s.voters...),
		voterSet:         make(map[string]struct{}, len(s.voterSet)),
		quorum:           s.quorum,
		committed:        s.committed,
		committedDigests: make(map[uint64]string, len(s.committedDigests)),
		sealed:           s.sealed,
		lastIndex:        s.lastIndex,
		lastActivity:     s.lastActivity,
		completed:        s.completed,
		highestObserved:  s.highestObserved,
		entries:          make(map[uint64]*pendingEntry, len(s.entries)),
		acks:             make(map[uint64]map[string]string, len(s.acks)),
		pendingAcks:      s.pendingAcks,
		bufferedBytes:    s.bufferedBytes,
	}
	for v := range s.voterSet {
		c.voterSet[v] = struct{}{}
	}
	for k, v := range s.committedDigests {
		c.committedDigests[k] = v
	}
	for k, v := range s.entries {
		c.entries[k] = v // pendingEntry is immutable
	}
	for k, v := range s.acks {
		inner := make(map[string]string, len(v))
		for r, d := range v {
			inner[r] = d
		}
		c.acks[k] = inner
	}
	return c
}

// Tracker is a multi-stream quorum commit tracker. All methods are safe for
// concurrent use.
type Tracker struct {
	mu               sync.Mutex
	maxBufferedBytes int
	streams          map[string]*stream
	bufferedBytes    int
}

func New(opts Options) (*Tracker, error) {
	if opts.MaxBufferedBytes <= 0 {
		return nil, ErrInvalid
	}
	return &Tracker{
		maxBufferedBytes: opts.MaxBufferedBytes,
		streams:          make(map[string]*stream),
	}, nil
}

func validateConfig(config StreamConfig) error {
	if !validIdent(config.ID) {
		return ErrInvalid
	}
	if len(config.Voters) == 0 {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(config.Voters))
	for _, v := range config.Voters {
		if !validIdent(v) {
			return ErrInvalid
		}
		if _, dup := seen[v]; dup {
			return ErrInvalid
		}
		seen[v] = struct{}{}
	}
	if config.Quorum < 1 || config.Quorum > len(config.Voters) {
		return ErrInvalid
	}
	return nil
}

func (t *Tracker) Open(config StreamConfig, at int64) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	if !validTime(at) {
		return ErrInvalid
	}
	voters := append([]string(nil), config.Voters...)
	sort.Strings(voters)

	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.streams[config.ID]; ok {
		if s.quorum != config.Quorum || !equalStrings(s.voters, voters) {
			return ErrConflict
		}
		if at < s.lastActivity {
			return ErrTime
		}
		s.lastActivity = at
		return nil
	}
	set := make(map[string]struct{}, len(voters))
	for _, v := range voters {
		set[v] = struct{}{}
	}
	t.streams[config.ID] = &stream{
		id:               config.ID,
		voters:           voters,
		voterSet:         set,
		quorum:           config.Quorum,
		committedDigests: make(map[uint64]string),
		lastActivity:     at,
		entries:          make(map[uint64]*pendingEntry),
		acks:             make(map[uint64]map[string]string),
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (t *Tracker) Apply(update Update) (Outcome, error) {
	outcomes, err := t.ApplyBatch([]Update{update})
	if err != nil {
		return Outcome{}, err
	}
	return outcomes[0], nil
}

func (t *Tracker) ApplyBatch(updates []Update) ([]Outcome, error) {
	outcomes := make([]Outcome, len(updates))
	if len(updates) == 0 {
		return outcomes, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	work := make(map[string]*stream, len(t.streams))
	for id, s := range t.streams {
		work[id] = s.clone()
	}
	buffered := t.bufferedBytes
	for i, u := range updates {
		o, err := applyUpdate(work, &buffered, u)
		if err != nil {
			return nil, err
		}
		outcomes[i] = o
	}
	if buffered > t.maxBufferedBytes {
		return nil, ErrCapacity
	}
	t.streams = work
	t.bufferedBytes = buffered
	return outcomes, nil
}

func applyUpdate(streams map[string]*stream, total *int, u Update) (Outcome, error) {
	kinds := 0
	if u.Entry != nil {
		kinds++
	}
	if u.Ack != nil {
		kinds++
	}
	if u.Seal != nil {
		kinds++
	}
	if kinds != 1 || !validIdent(u.Stream) || !validTime(u.At) {
		return Outcome{}, ErrInvalid
	}
	if u.Entry != nil && (!validIndex(u.Entry.Index) || !validIdent(u.Entry.Digest)) {
		return Outcome{}, ErrInvalid
	}
	if u.Ack != nil && (!validIndex(u.Ack.Index) || !validIdent(u.Ack.Replica) || !validIdent(u.Ack.Digest)) {
		return Outcome{}, ErrInvalid
	}
	if u.Seal != nil && u.Seal.LastIndex > maxIndex {
		return Outcome{}, ErrInvalid
	}
	s, ok := streams[u.Stream]
	if !ok {
		return Outcome{}, ErrUnknownStream
	}
	if u.Ack != nil {
		if _, voter := s.voterSet[u.Ack.Replica]; !voter {
			return Outcome{}, ErrInvalid
		}
	}
	if u.At < s.lastActivity {
		return Outcome{}, ErrTime
	}
	var o Outcome
	var err error
	switch {
	case u.Entry != nil:
		o, err = s.applyEntry(total, u.Entry)
	case u.Ack != nil:
		o, err = s.applyAck(total, u.Ack)
	default:
		o, err = s.applySeal(u.Seal)
	}
	if err != nil {
		return Outcome{}, err
	}
	s.lastActivity = u.At
	return o, nil
}

func (s *stream) applyEntry(total *int, e *Entry) (Outcome, error) {
	if s.sealed && e.Index > s.lastIndex {
		return Outcome{}, ErrSealed
	}
	if e.Index <= s.committed {
		if s.committedDigests[e.Index] == e.Digest {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	if p, ok := s.entries[e.Index]; ok {
		if p.digest == e.Digest && bytes.Equal(p.payload, e.Payload) {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	payload := append([]byte(nil), e.Payload...)
	s.entries[e.Index] = &pendingEntry{digest: e.Digest, payload: payload}
	s.bufferedBytes += len(payload)
	*total += len(payload)
	if e.Index > s.highestObserved {
		s.highestObserved = e.Index
	}
	return s.commitLoop(total), nil
}

func (s *stream) applyAck(total *int, a *Ack) (Outcome, error) {
	if s.sealed && a.Index > s.lastIndex {
		return Outcome{}, ErrSealed
	}
	if a.Index <= s.committed {
		if s.committedDigests[a.Index] == a.Digest {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	m := s.acks[a.Index]
	if m == nil {
		m = make(map[string]string)
		s.acks[a.Index] = m
	} else if d, ok := m[a.Replica]; ok {
		if d == a.Digest {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	m[a.Replica] = a.Digest
	s.pendingAcks++
	if a.Index > s.highestObserved {
		s.highestObserved = a.Index
	}
	return s.commitLoop(total), nil
}

func (s *stream) applySeal(se *Seal) (Outcome, error) {
	if s.sealed {
		if s.lastIndex == se.LastIndex {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	if se.LastIndex < s.highestObserved || se.LastIndex < s.committed {
		return Outcome{}, ErrConflict
	}
	s.sealed = true
	s.lastIndex = se.LastIndex
	if se.LastIndex > s.highestObserved {
		s.highestObserved = se.LastIndex
	}
	var o Outcome
	if !s.completed && s.committed == s.lastIndex {
		s.completed = true
		o.Completed = true
	}
	return o, nil
}

func (s *stream) commitLoop(total *int) Outcome {
	var o Outcome
	for {
		next := s.committed + 1
		p, ok := s.entries[next]
		if !ok {
			break
		}
		matching := 0
		for _, digest := range s.acks[next] {
			if digest == p.digest {
				matching++
			}
		}
		if matching < s.quorum {
			break
		}
		s.committed = next
		s.committedDigests[next] = p.digest
		delete(s.entries, next)
		if m := s.acks[next]; m != nil {
			s.pendingAcks -= len(m)
			delete(s.acks, next)
		}
		s.bufferedBytes -= len(p.payload)
		*total -= len(p.payload)
		o.Committed = append(o.Committed, CommittedEntry{
			Stream:  s.id,
			Index:   next,
			Digest:  p.digest,
			Payload: p.payload,
		})
	}
	if s.sealed && !s.completed && s.committed == s.lastIndex {
		s.completed = true
		o.Completed = true
	}
	return o
}

func (t *Tracker) ExpireBefore(cutoff int64) ([]ExpiredStream, error) {
	if !validTime(cutoff) {
		return nil, ErrInvalid
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var expired []ExpiredStream
	for id, s := range t.streams {
		if s.lastActivity < cutoff {
			expired = append(expired, ExpiredStream{
				Stream:        id,
				Committed:     s.committed,
				BufferedBytes: s.bufferedBytes,
			})
		}
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i].Stream < expired[j].Stream })
	for _, e := range expired {
		t.bufferedBytes -= t.streams[e.Stream].bufferedBytes
		delete(t.streams, e.Stream)
	}
	if expired == nil {
		expired = []ExpiredStream{}
	}
	return expired, nil
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := make([]string, 0, len(t.streams))
	for id := range t.streams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	snap := Snapshot{
		Streams:       make([]StreamState, 0, len(ids)),
		BufferedBytes: t.bufferedBytes,
	}
	for _, id := range ids {
		s := t.streams[id]
		snap.Streams = append(snap.Streams, StreamState{
			ID:             s.id,
			Voters:         append([]string(nil), s.voters...),
			Quorum:         s.quorum,
			Committed:      s.committed,
			Sealed:         s.sealed,
			LastIndex:      s.lastIndex,
			LastActivity:   s.lastActivity,
			PendingEntries: len(s.entries),
			PendingAcks:    s.pendingAcks,
			BufferedBytes:  s.bufferedBytes,
			Completed:      s.completed,
		})
	}
	return snap
}
