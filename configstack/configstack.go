package configstack

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	AddLayer Kind = iota + 1
	RemoveLayer
	Set
	Delete
	Move
)

type Options struct{ MaxLayers, MaxEntries, MaxNameBytes, MaxKeyBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind      Kind
	Name, Key string
	Value     []byte
	Position  int
}
type Batch struct{ Ops []Op }
type Entry struct {
	Layer, Key string
	Value      []byte
	Revision   uint64
}
type Layer struct {
	Name    string
	Entries []Entry
}
type Result struct {
	Generation, Revision uint64
	Changed              []Entry
}
type Snapshot struct {
	Generation, NextRevision uint64
	Layers                   []Layer
}
type Stack struct{}

func New(Options) (*Stack, error)                  { return nil, ErrNotImplemented }
func (*Stack) Apply(Batch) (Result, error)         { return Result{}, ErrNotImplemented }
func (*Stack) Resolve(string) (Entry, bool, error) { return Entry{}, false, ErrNotImplemented }
func (*Stack) Snapshot() Snapshot                  { return Snapshot{} }
