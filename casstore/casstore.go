package casstore

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type CompareKind uint8

const (
	Exists CompareKind = iota + 1
	NotExists
	Revision
	Value
)

type WriteKind uint8

const (
	Put WriteKind = iota + 1
	Delete
)

type Options struct{ MaxKeys, MaxValueBytes, MaxNameBytes int }
type Compare struct {
	Kind     CompareKind
	Key      string
	Revision uint64
	Value    []byte
}
type Write struct {
	Kind  WriteKind
	Key   string
	Value []byte
}
type Txn struct {
	Compares []Compare
	Writes   []Write
}
type Entry struct {
	Key      string
	Revision uint64
	Value    []byte
}
type TxnResult struct {
	Succeeded            bool
	Generation, Revision uint64
}
type Snapshot struct {
	Generation, NextRevision uint64
	Keys, ValueBytes         int
	Entries                  []Entry
}
type Store struct{}

func New(Options) (*Store, error)                        { return nil, ErrInvalidOptions }
func (*Store) Transact(Txn) (TxnResult, error)           { return TxnResult{}, ErrInvalidInput }
func (*Store) Get(string) (Entry, bool, error)           { return Entry{}, false, ErrInvalidInput }
func (*Store) List(string, string, int) ([]Entry, error) { return nil, ErrInvalidInput }
func (*Store) Snapshot() Snapshot                        { return Snapshot{} }
