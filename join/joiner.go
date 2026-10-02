// Package join implements an event-time two-stream correlator.
package join

import "errors"

var (
	ErrInvalid        = errors.New("join: invalid input")
	ErrTime           = errors.New("join: watermark moved backwards")
	ErrLate           = errors.New("join: event is late")
	ErrConflict       = errors.New("join: event id conflict")
	ErrCapacity       = errors.New("join: capacity exceeded")
	ErrNotImplemented = errors.New("join: not implemented")
)

type Side uint8

const (
	Left Side = iota + 1
	Right
)

type Options struct {
	Window    int64
	MaxEvents int
	MaxBytes  int
}

type Event struct {
	Side    Side
	Key     string
	ID      string
	Time    int64
	Payload []byte
}

type Watermark struct {
	Side Side
	Time int64
}

// Update must contain exactly one of Event or Watermark.
type Update struct {
	Event     *Event
	Watermark *Watermark
}

type Match struct {
	Key       string
	LeftID    string
	RightID   string
	LeftTime  int64
	RightTime int64
	Left      []byte
	Right     []byte
}

type Expired struct {
	Side Side
	Key  string
	ID   string
	Time int64
}

type Outcome struct {
	Matches []Match
	Expired []Expired
}

type EventState struct {
	Side  Side
	Key   string
	ID    string
	Time  int64
	Bytes int
}

type Snapshot struct {
	LeftWatermark  int64
	RightWatermark int64
	Events         []EventState
	Count          int
	Bytes          int
}

type Joiner struct{}

func New(opts Options) (*Joiner, error) { return nil, ErrNotImplemented }

func (j *Joiner) Apply(update Update) (Outcome, error) {
	return Outcome{}, ErrNotImplemented
}

func (j *Joiner) ApplyBatch(updates []Update) ([]Outcome, error) {
	return nil, ErrNotImplemented
}

func (j *Joiner) Snapshot() Snapshot { return Snapshot{} }
