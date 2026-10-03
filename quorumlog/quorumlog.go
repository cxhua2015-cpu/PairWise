// Package quorumlog tracks quorum commits for multiple replicated log streams.
package quorumlog

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalid       = errors.New("quorumlog: invalid input")
	ErrUnknownStream = errors.New("quorumlog: unknown stream")
	ErrTime          = errors.New("quorumlog: activity time moved backwards")
	ErrConflict      = errors.New("quorumlog: conflicting observation")
	ErrSealed        = errors.New("quorumlog: index beyond seal")
	ErrCapacity      = errors.New("quorumlog: capacity exceeded")
)

const (
	maxNameLen = 128
	maxTime    = 1_000_000_000_000
	maxIndex   = 1_000_000_000_000
)

type Options struct {
	MaxBufferedBytes int
}

type StreamConfig struct {
	ID     string
	Voters []string
	Quorum int
}

type Entry struct {
	Index   uint64
	Digest  string
	Payload []byte
}

type Ack struct {
	Index   uint64
	Replica string
	Digest  string
}

type Seal struct {
	LastIndex uint64
}

// Update must contain exactly one of Entry, Ack, or Seal.
type Update struct {
	Stream string
	At     int64
	Entry  *Entry
	Ack    *Ack
	Seal   *Seal
}

type CommittedEntry struct {
	Stream  string
	Index   uint64
	Digest  string
	Payload []byte
}

type Outcome struct {
	Committed []CommittedEntry
	Completed bool
}

type ExpiredStream struct {
	Stream        string
	Committed     uint64
	BufferedBytes int
}

type StreamState struct {
	ID             string
	Voters         []string
	Quorum         int
	Committed      uint64
	Sealed         bool
	LastIndex      uint64
	LastActivity   int64
	PendingEntries int
	PendingAcks    int
	BufferedBytes  int
	Completed      bool
}

type Snapshot struct {
	Streams       []StreamState
	BufferedBytes int
}

func validName(s string) bool      { return len(s) >= 1 && len(s) <= maxNameLen }
func validTime(v int64) bool       { return v >= 0 && v <= maxTime }
func validIndex(v uint64) bool     { return v >= 1 && v <= maxIndex }
func validLastIndex(v uint64) bool { return v <= maxIndex }

// pendingEntry is an uncommitted log entry. payload is owned by the stream
// and never mutated in place.
type pendingEntry struct {
	digest  string
	payload []byte
}

// stream is the mutable per-stream state. voters and voterSet are immutable
// after creation; everything else may be mutated by a batch clone.
type stream struct {
	id           string
	voters       []string
	voterSet     map[string]struct{}
	quorum       int
	committed    uint64
	digests      map[uint64]string // committed index -> digest, retained forever
	sealed       bool
	lastIndex    uint64
	lastActivity int64
	entries      map[uint64]pendingEntry
	acks         map[uint64]map[string]string // index -> replica -> digest
	maxObserved  uint64
	buffered     int
	completed    bool
}

func newStream(config StreamConfig, at int64) *stream {
	voters := make([]string, len(config.Voters))
	copy(voters, config.Voters)
	sort.Strings(voters)
	set := make(map[string]struct{}, len(voters))
	for _, v := range voters {
		set[v] = struct{}{}
	}
	return &stream{
		id:           config.ID,
		voters:       voters,
		voterSet:     set,
		quorum:       config.Quorum,
		digests:      make(map[uint64]string),
		entries:      make(map[uint64]pendingEntry),
		acks:         make(map[uint64]map[string]string),
		lastActivity: at,
	}
}

// clone deep-copies the mutable bookkeeping so a batch can mutate the copy
// and discard it on failure. Payload byte slices are shared because they are
// never mutated in place; outcome payloads are always fresh copies.
func (s *stream) clone() *stream {
	c := *s
	c.entries = make(map[uint64]pendingEntry, len(s.entries))
	for k, v := range s.entries {
		c.entries[k] = v
	}
	c.acks = make(map[uint64]map[string]string, len(s.acks))
	for k, v := range s.acks {
		m := make(map[string]string, len(v))
		for r, d := range v {
			m[r] = d
		}
		c.acks[k] = m
	}
	c.digests = make(map[uint64]string, len(s.digests))
	for k, v := range s.digests {
		c.digests[k] = v
	}
	return &c
}

func (s *stream) pendingAcks() int {
	n := 0
	for _, m := range s.acks {
		n += len(m)
	}
	return n
}

// Tracker is a multi-stream quorum commit tracker. All methods are safe for
// concurrent use.
type Tracker struct {
	mu       sync.Mutex
	max      int
	streams  map[string]*stream
	buffered int
}

func New(opts Options) (*Tracker, error) {
	if opts.MaxBufferedBytes <= 0 {
		return nil, ErrInvalid
	}
	return &Tracker{
		max:     opts.MaxBufferedBytes,
		streams: make(map[string]*stream),
	}, nil
}

func (t *Tracker) Open(config StreamConfig, at int64) error {
	if !validName(config.ID) || !validTime(at) {
		return ErrInvalid
	}
	if len(config.Voters) == 0 || config.Quorum < 1 || config.Quorum > len(config.Voters) {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(config.Voters))
	for _, v := range config.Voters {
		if !validName(v) {
			return ErrInvalid
		}
		if _, dup := seen[v]; dup {
			return ErrInvalid
		}
		seen[v] = struct{}{}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.streams[config.ID]; ok {
		if s.quorum != config.Quorum || !equalStrings(s.voters, sortedCopy(config.Voters)) {
			return ErrConflict
		}
		if at < s.lastActivity {
			return ErrTime
		}
		s.lastActivity = at
		return nil
	}
	t.streams[config.ID] = newStream(config, at)
	return nil
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
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

func validateUpdate(u Update) error {
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
	if kinds != 1 {
		return ErrInvalid
	}
	if !validName(u.Stream) || !validTime(u.At) {
		return ErrInvalid
	}
	switch {
	case u.Entry != nil:
		if !validIndex(u.Entry.Index) || !validName(u.Entry.Digest) {
			return ErrInvalid
		}
	case u.Ack != nil:
		if !validIndex(u.Ack.Index) || !validName(u.Ack.Replica) || !validName(u.Ack.Digest) {
			return ErrInvalid
		}
	case u.Seal != nil:
		if !validLastIndex(u.Seal.LastIndex) {
			return ErrInvalid
		}
	}
	return nil
}

func (t *Tracker) ApplyBatch(updates []Update) ([]Outcome, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	outcomes := make([]Outcome, 0, len(updates))
	if len(updates) == 0 {
		return outcomes, nil
	}

	// Transactional state: clones of touched streams plus a working copy of
	// the global buffered counter. Nothing becomes visible until every
	// update succeeds and the final capacity check passes.
	clones := make(map[string]*stream)
	buffered := t.buffered

	for i := range updates {
		u := updates[i]
		if err := validateUpdate(u); err != nil {
			return nil, err
		}
		s, ok := clones[u.Stream]
		if !ok {
			orig, open := t.streams[u.Stream]
			if !open {
				return nil, ErrUnknownStream
			}
			s = orig.clone()
			clones[u.Stream] = s
		}
		before := s.buffered
		out, err := s.apply(u)
		if err != nil {
			return nil, err
		}
		buffered += s.buffered - before
		outcomes = append(outcomes, out)
	}

	if buffered > t.max {
		return nil, ErrCapacity
	}
	for id, s := range clones {
		t.streams[id] = s
	}
	t.buffered = buffered
	return outcomes, nil
}

// apply validates and applies one update against the (possibly cloned)
// stream, advancing its activity time on success.
func (s *stream) apply(u Update) (Outcome, error) {
	if u.At < s.lastActivity {
		return Outcome{}, ErrTime
	}
	var out Outcome
	var err error
	switch {
	case u.Entry != nil:
		out, err = s.applyEntry(u.Entry)
	case u.Ack != nil:
		out, err = s.applyAck(u.Ack)
	default:
		out, err = s.applySeal(u.Seal)
	}
	if err != nil {
		return Outcome{}, err
	}
	s.lastActivity = u.At
	return out, nil
}

func (s *stream) applyEntry(e *Entry) (Outcome, error) {
	if s.sealed && e.Index > s.lastIndex {
		return Outcome{}, ErrSealed
	}
	if e.Index <= s.committed {
		if s.digests[e.Index] == e.Digest {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	if pe, ok := s.entries[e.Index]; ok {
		if pe.digest == e.Digest && bytes.Equal(pe.payload, e.Payload) {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	payload := make([]byte, len(e.Payload))
	copy(payload, e.Payload)
	s.entries[e.Index] = pendingEntry{digest: e.Digest, payload: payload}
	s.buffered += len(payload)
	if e.Index > s.maxObserved {
		s.maxObserved = e.Index
	}
	var out Outcome
	s.commit(&out)
	return out, nil
}

func (s *stream) applyAck(a *Ack) (Outcome, error) {
	if _, ok := s.voterSet[a.Replica]; !ok {
		return Outcome{}, ErrInvalid
	}
	if s.sealed && a.Index > s.lastIndex {
		return Outcome{}, ErrSealed
	}
	if a.Index <= s.committed {
		if s.digests[a.Index] == a.Digest {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	m, ok := s.acks[a.Index]
	if ok {
		if d, dup := m[a.Replica]; dup {
			if d == a.Digest {
				return Outcome{}, nil
			}
			return Outcome{}, ErrConflict
		}
	} else {
		m = make(map[string]string)
		s.acks[a.Index] = m
	}
	m[a.Replica] = a.Digest
	if a.Index > s.maxObserved {
		s.maxObserved = a.Index
	}
	var out Outcome
	s.commit(&out)
	return out, nil
}

func (s *stream) applySeal(sl *Seal) (Outcome, error) {
	if s.sealed {
		if sl.LastIndex == s.lastIndex {
			return Outcome{}, nil
		}
		return Outcome{}, ErrConflict
	}
	if sl.LastIndex < s.committed || sl.LastIndex < s.maxObserved {
		return Outcome{}, ErrConflict
	}
	s.sealed = true
	s.lastIndex = sl.LastIndex
	var out Outcome
	s.checkCompleted(&out)
	return out, nil
}

// commit advances the committed prefix while Committed+1 has its entry
// present and quorum-matching ACKs, then evaluates completion.
func (s *stream) commit(out *Outcome) {
	for {
		idx := s.committed + 1
		pe, ok := s.entries[idx]
		if !ok {
			break
		}
		matching := 0
		for _, d := range s.acks[idx] {
			if d == pe.digest {
				matching++
			}
		}
		if matching < s.quorum {
			break
		}
		payload := make([]byte, len(pe.payload))
		copy(payload, pe.payload)
		out.Committed = append(out.Committed, CommittedEntry{
			Stream:  s.id,
			Index:   idx,
			Digest:  pe.digest,
			Payload: payload,
		})
		s.digests[idx] = pe.digest
		s.committed = idx
		s.buffered -= len(pe.payload)
		delete(s.entries, idx)
		delete(s.acks, idx)
	}
	s.checkCompleted(out)
}

func (s *stream) checkCompleted(out *Outcome) {
	if s.sealed && !s.completed && s.committed == s.lastIndex {
		s.completed = true
		out.Completed = true
	}
}

func (t *Tracker) ExpireBefore(cutoff int64) ([]ExpiredStream, error) {
	if !validTime(cutoff) {
		return nil, ErrInvalid
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var ids []string
	for id, s := range t.streams {
		if s.lastActivity < cutoff {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sort.Strings(ids)
	expired := make([]ExpiredStream, 0, len(ids))
	for _, id := range ids {
		s := t.streams[id]
		expired = append(expired, ExpiredStream{
			Stream:        id,
			Committed:     s.committed,
			BufferedBytes: s.buffered,
		})
		t.buffered -= s.buffered
		delete(t.streams, id)
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
		BufferedBytes: t.buffered,
	}
	for _, id := range ids {
		s := t.streams[id]
		voters := make([]string, len(s.voters))
		copy(voters, s.voters)
		snap.Streams = append(snap.Streams, StreamState{
			ID:             s.id,
			Voters:         voters,
			Quorum:         s.quorum,
			Committed:      s.committed,
			Sealed:         s.sealed,
			LastIndex:      s.lastIndex,
			LastActivity:   s.lastActivity,
			PendingEntries: len(s.entries),
			PendingAcks:    s.pendingAcks(),
			BufferedBytes:  s.buffered,
			Completed:      s.completed,
		})
	}
	return snap
}
