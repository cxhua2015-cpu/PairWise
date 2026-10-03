// Package quorumlog tracks quorum commits for multiple replicated log streams.
package quorumlog

import "errors"

var (
	ErrInvalid        = errors.New("quorumlog: invalid input")
	ErrUnknownStream  = errors.New("quorumlog: unknown stream")
	ErrTime           = errors.New("quorumlog: activity time moved backwards")
	ErrConflict       = errors.New("quorumlog: conflicting observation")
	ErrSealed         = errors.New("quorumlog: index beyond seal")
	ErrCapacity       = errors.New("quorumlog: capacity exceeded")
	ErrNotImplemented = errors.New("quorumlog: not implemented")
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
