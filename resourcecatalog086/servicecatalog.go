package resourcecatalog086

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxRecords, MaxNameBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind  Kind
	Name  string
	Value []byte
}
type Batch struct{ Ops []Op }
type Record struct {
	Name     string
	Value    []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Record
}
type Snapshot struct {
	Generation, NextRevision uint64
	Records                  []Record
}
type Store struct{}

func New(Options) (*Store, error)               { return nil, ErrNotImplemented }
func (*Store) Apply(Batch) (Result, error)      { return Result{}, ErrNotImplemented }
func (*Store) Get(string) (Record, bool, error) { return Record{}, false, ErrNotImplemented }
func (*Store) Snapshot() Snapshot               { return Snapshot{} }
