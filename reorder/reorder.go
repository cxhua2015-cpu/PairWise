package reorder

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidEvent   = errors.New("invalid event")
	ErrOldSequence    = errors.New("old sequence")
	ErrConflict       = errors.New("sequence conflict")
	ErrCapacity       = errors.New("capacity exceeded")
	ErrNotFound       = errors.New("stream not found")
	ErrNotEmpty       = errors.New("stream not empty")
)

type Options struct{ MaxStreams, MaxBuffered, MaxPayloadBytes, MaxStreamBytes int }
type Event struct {
	Stream   string
	Sequence uint64
	Payload  []byte
}
type StreamSnapshot struct {
	Stream   string
	Next     uint64
	Buffered []Event
}
type Snapshot struct {
	Generation                      uint64
	Streams, Buffered, PayloadBytes int
	State                           []StreamSnapshot
}

type stream struct {
	next uint64
	buf  map[uint64][]byte
}

type Buffer struct {
	mu      sync.Mutex
	opts    Options
	streams map[string]*stream
	gen     uint64
}

func New(opts Options) (*Buffer, error) {
	if opts.MaxStreams <= 0 || opts.MaxBuffered <= 0 || opts.MaxPayloadBytes <= 0 || opts.MaxStreamBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Buffer{opts: opts, streams: make(map[string]*stream)}, nil
}

func (b *Buffer) validStream(s string) bool {
	return len(s) > 0 && len(s) <= b.opts.MaxStreamBytes
}

func (b *Buffer) validEvent(e Event) bool {
	return b.validStream(e.Stream) && e.Sequence >= 1 && e.Sequence <= math.MaxUint64-1 && e.Payload != nil
}

func cloneStream(s *stream) *stream {
	c := &stream{next: s.next, buf: make(map[uint64][]byte, len(s.buf))}
	for k, v := range s.buf {
		c.buf[k] = v // payloads are immutable once stored; cloned on commit
	}
	return c
}

// PushBatch validates, applies to an isolated candidate, checks capacity on
// the final candidate, and commits atomically.
func (b *Buffer) PushBatch(events []Event) ([]Event, error) {
	if len(events) == 0 {
		return nil, nil
	}
	for _, e := range events {
		if !b.validEvent(e) {
			return nil, ErrInvalidEvent
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Isolated candidate: clone per-stream state lazily (copy-on-write).
	cand := make(map[string]*stream, len(b.streams))
	for name, s := range b.streams {
		cand[name] = s
	}
	cloned := make(map[string]bool)
	get := func(name string) *stream {
		s := cand[name]
		if s == nil {
			s = &stream{next: 1, buf: make(map[uint64][]byte)}
			cand[name] = s
			cloned[name] = true
			return s
		}
		if !cloned[name] {
			s = cloneStream(s)
			cand[name] = s
			cloned[name] = true
		}
		return s
	}

	var ready []Event
	changed := false
	for _, e := range events {
		s := get(e.Stream)
		switch {
		case e.Sequence < s.next:
			return nil, ErrOldSequence
		case e.Sequence > s.next:
			if old, ok := s.buf[e.Sequence]; ok {
				if bytes.Equal(old, e.Payload) {
					continue // idempotent no-op
				}
				return nil, ErrConflict
			}
			s.buf[e.Sequence] = bytes.Clone(e.Payload)
			changed = true
		default: // e.Sequence == s.next
			ready = append(ready, Event{Stream: e.Stream, Sequence: e.Sequence, Payload: bytes.Clone(e.Payload)})
			s.next++
			changed = true
			for {
				p, ok := s.buf[s.next]
				if !ok {
					break
				}
				delete(s.buf, s.next)
				ready = append(ready, Event{Stream: e.Stream, Sequence: s.next, Payload: bytes.Clone(p)})
				s.next++
			}
		}
	}

	// Capacity checks on the final candidate state only.
	if len(cand) > b.opts.MaxStreams {
		return nil, ErrCapacity
	}
	buffered, payloadBytes := 0, 0
	for _, s := range cand {
		buffered += len(s.buf)
		for _, p := range s.buf {
			payloadBytes += len(p)
		}
	}
	if buffered > b.opts.MaxBuffered || payloadBytes > b.opts.MaxPayloadBytes {
		return nil, ErrCapacity
	}

	if changed {
		for name, s := range cand {
			if cloned[name] {
				b.streams[name] = s
			}
		}
		b.gen++
	}
	return ready, nil
}

func (b *Buffer) Skip(stream string, through uint64) ([]Event, error) {
	if !b.validStream(stream) || through < 1 || through > math.MaxUint64-1 {
		return nil, ErrInvalidEvent
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.streams[stream]
	if s == nil {
		return nil, ErrNotFound
	}
	if through < s.next {
		return nil, nil
	}
	for seq := range s.buf {
		if seq <= through {
			delete(s.buf, seq)
		}
	}
	s.next = through + 1
	var ready []Event
	for {
		p, ok := s.buf[s.next]
		if !ok {
			break
		}
		delete(s.buf, s.next)
		ready = append(ready, Event{Stream: stream, Sequence: s.next, Payload: bytes.Clone(p)})
		s.next++
	}
	b.gen++
	return ready, nil
}

func (b *Buffer) Delete(stream string) error {
	if !b.validStream(stream) {
		return ErrInvalidEvent
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.streams[stream]
	if s == nil {
		return ErrNotFound
	}
	if len(s.buf) > 0 {
		return ErrNotEmpty
	}
	delete(b.streams, stream)
	b.gen++
	return nil
}

func (b *Buffer) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	snap := Snapshot{Generation: b.gen, Streams: len(b.streams)}
	names := make([]string, 0, len(b.streams))
	for name := range b.streams {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := b.streams[name]
		ss := StreamSnapshot{Stream: name, Next: s.next}
		seqs := make([]uint64, 0, len(s.buf))
		for seq := range s.buf {
			seqs = append(seqs, seq)
		}
		sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
		for _, seq := range seqs {
			p := s.buf[seq]
			ss.Buffered = append(ss.Buffered, Event{Stream: name, Sequence: seq, Payload: bytes.Clone(p)})
			snap.Buffered++
			snap.PayloadBytes += len(p)
		}
		snap.State = append(snap.State, ss)
	}
	return snap
}
