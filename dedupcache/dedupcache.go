package dedupcache

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxEntries, MaxKeyBytes, MaxTokenBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind       Kind
	Key, Token string
	Value      []byte
	ExpiresAt  int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Record struct {
	Key, Token string
	Value      []byte
	ExpiresAt  int64
	Revision   uint64
}
type Outcome struct {
	Key             string
	Created, Replay bool
	Record          Record
}
type Result struct {
	Generation, Revision uint64
	Outcomes             []Outcome
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Records                  []Record
}
type Cache struct{}

func New(Options) (*Cache, error)                      { return nil, ErrNotImplemented }
func (*Cache) Apply(Batch) (Result, error)             { return Result{}, ErrNotImplemented }
func (*Cache) Get(int64, string) (Record, bool, error) { return Record{}, false, ErrNotImplemented }
func (*Cache) Snapshot() Snapshot                      { return Snapshot{} }
