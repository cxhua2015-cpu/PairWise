package expiringstore

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	MaxEntries, MaxTotalBytes, MaxValueBytes, MaxKeyBytes int
}

type OpKind uint8

const (
	Put OpKind = iota + 1
	Delete
	Touch
)

type Op struct {
	Kind      OpKind
	Key       string
	Value     []byte
	ExpiresAt int64
}

type Batch struct {
	Now int64
	Ops []Op
}

type Result struct {
	Generation, Revision uint64
	Expired              []string
}

type Entry struct {
	Key       string
	Value     []byte
	Revision  uint64
	ExpiresAt int64
}

type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	TotalBytes               int
	Entries                  []Entry
}

type Store struct{}

func New(Options) (*Store, error)                     { return nil, ErrInvalidOptions }
func (*Store) Apply(Batch) (Result, error)             { return Result{}, ErrInvalidInput }
func (*Store) Get(string, int64) (Entry, bool, error)  { return Entry{}, false, ErrInvalidInput }
func (*Store) Sweep(int64) ([]string, error)           { return nil, ErrInvalidInput }
func (*Store) Snapshot() Snapshot                      { return Snapshot{} }
