package reorder

import "errors"

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
type Buffer struct{}

func New(opts Options) (*Buffer, error)                               { return nil, ErrInvalidOptions }
func (b *Buffer) PushBatch(events []Event) ([]Event, error)           { return nil, ErrInvalidEvent }
func (b *Buffer) Skip(stream string, through uint64) ([]Event, error) { return nil, ErrNotFound }
func (b *Buffer) Delete(stream string) error                          { return ErrNotFound }
func (b *Buffer) Snapshot() Snapshot                                  { return Snapshot{} }
