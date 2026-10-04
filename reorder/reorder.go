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

type streamState struct {
	next uint64
	buf  map[uint64][]byte
}

type Buffer struct {
	mu           sync.Mutex
	opts         Options
	streams      map[string]*streamState
	buffered     int
	payloadBytes int
	generation   uint64
}

func New(opts Options) (*Buffer, error) {
	if opts.MaxStreams <= 0 || opts.MaxBuffered <= 0 || opts.MaxPayloadBytes <= 0 || opts.MaxStreamBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Buffer{opts: opts, streams: make(map[string]*streamState)}, nil
}

func (b *Buffer) validStream(s string) bool {
	return len(s) > 0 && len(s) <= b.opts.MaxStreamBytes
}

func (b *Buffer) validSequence(seq uint64) bool {
	return seq >= 1 && seq <= math.MaxUint64-1
}

func (b *Buffer) validateEvent(e Event) bool {
	return b.validStream(e.Stream) && b.validSequence(e.Sequence) && e.Payload != nil
}

// clone returns a deep copy of the registry; payload slices are shared but
// treated as immutable (all accepted payloads are private copies).
func (b *Buffer) clone() map[string]*streamState {
	out := make(map[string]*streamState, len(b.streams))
	for name, st := range b.streams {
		nb := make(map[uint64][]byte, len(st.buf))
		for seq, p := range st.buf {
			nb[seq] = p
		}
		out[name] = &streamState{next: st.next, buf: nb}
	}
	return out
}

func (b *Buffer) PushBatch(events []Event) ([]Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, e := range events {
		if !b.validateEvent(e) {
			return nil, ErrInvalidEvent
		}
	}
	if len(events) == 0 {
		return nil, nil
	}

	cand := b.clone()
	buffered, payloadBytes := b.buffered, b.payloadBytes
	changed := false
	var ready []Event

	// process events in input order
	for _, e := range events {
		st, ok := cand[e.Stream]
		if !ok {
			st = &streamState{next: 1, buf: make(map[uint64][]byte)}
			cand[e.Stream] = st
			changed = true
		}
		switch {
		case e.Sequence < st.next:
			return nil, ErrOldSequence
		case e.Sequence == st.next:
			ready = append(ready, Event{Stream: e.Stream, Sequence: e.Sequence, Payload: append([]byte(nil), e.Payload...)})
			st.next++
			changed = true
			for {
				p, ok := st.buf[st.next]
				if !ok {
					break
				}
				delete(st.buf, st.next)
				buffered--
				payloadBytes -= len(p)
				ready = append(ready, Event{Stream: e.Stream, Sequence: st.next, Payload: append([]byte(nil), p...)})
				st.next++
			}
		default: // e.Sequence > st.next
			if old, ok := st.buf[e.Sequence]; ok {
				if bytes.Equal(old, e.Payload) {
					continue // idempotent no-op
				}
				return nil, ErrConflict
			}
			st.buf[e.Sequence] = append([]byte(nil), e.Payload...)
			buffered++
			payloadBytes += len(e.Payload)
			changed = true
		}
	}

	if len(cand) > b.opts.MaxStreams || buffered > b.opts.MaxBuffered || payloadBytes > b.opts.MaxPayloadBytes {
		return nil, ErrCapacity
	}

	b.streams = cand
	b.buffered = buffered
	b.payloadBytes = payloadBytes
	if changed {
		b.generation++
	}
	return ready, nil
}

func (b *Buffer) Skip(stream string, through uint64) ([]Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.validStream(stream) || !b.validSequence(through) {
		return nil, ErrInvalidEvent
	}
	st, ok := b.streams[stream]
	if !ok {
		return nil, ErrNotFound
	}
	if through < st.next {
		return nil, nil
	}
	for seq, p := range st.buf {
		if seq <= through {
			delete(st.buf, seq)
			b.buffered--
			b.payloadBytes -= len(p)
		}
	}
	st.next = through + 1
	var ready []Event
	for {
		p, ok := st.buf[st.next]
		if !ok {
			break
		}
		delete(st.buf, st.next)
		b.buffered--
		b.payloadBytes -= len(p)
		ready = append(ready, Event{Stream: stream, Sequence: st.next, Payload: append([]byte(nil), p...)})
		st.next++
	}
	b.generation++
	return ready, nil
}

func (b *Buffer) Delete(stream string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.validStream(stream) {
		return ErrInvalidEvent
	}
	st, ok := b.streams[stream]
	if !ok {
		return ErrNotFound
	}
	if len(st.buf) > 0 {
		return ErrNotEmpty
	}
	delete(b.streams, stream)
	b.generation++
	return nil
}

func (b *Buffer) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	snap := Snapshot{
		Generation:   b.generation,
		Streams:      len(b.streams),
		Buffered:     b.buffered,
		PayloadBytes: b.payloadBytes,
	}
	names := make([]string, 0, len(b.streams))
	for name := range b.streams {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := b.streams[name]
		ss := StreamSnapshot{Stream: name, Next: st.next}
		seqs := make([]uint64, 0, len(st.buf))
		for seq := range st.buf {
			seqs = append(seqs, seq)
		}
		sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
		for _, seq := range seqs {
			ss.Buffered = append(ss.Buffered, Event{Stream: name, Sequence: seq, Payload: append([]byte(nil), st.buf[seq]...)})
		}
		snap.State = append(snap.State, ss)
	}
	return snap
}
