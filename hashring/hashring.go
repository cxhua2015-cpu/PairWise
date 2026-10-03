package hashring

import "errors"

var (
	ErrNotImplemented = errors.New("hashring: not implemented")
	ErrInvalidOptions = errors.New("hashring: invalid options")
	ErrInvalidChange  = errors.New("hashring: invalid change")
	ErrInvalidID      = errors.New("hashring: invalid id")
	ErrInvalidWeight  = errors.New("hashring: invalid weight")
	ErrValueTooLarge  = errors.New("hashring: value too large")
	ErrDuplicate      = errors.New("hashring: duplicate node")
	ErrNotFound       = errors.New("hashring: node not found")
	ErrCapacity       = errors.New("hashring: capacity exceeded")
	ErrInvalidLookup  = errors.New("hashring: invalid lookup")
	ErrEmpty          = errors.New("hashring: empty ring")
)

type Hasher func([]byte) uint64
type Options struct {
	MaxNodes, MaxTokens, MaxValueBytes int
	Hasher                             Hasher
}
type Node struct {
	ID     string
	Weight int
	Value  []byte
}
type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeRemove
	ChangeUpdate
)

type Change struct {
	Type ChangeType
	Node Node
	ID   string
}
type Owner struct {
	ID    string
	Value []byte
}
type TokenView struct {
	Hash    uint64
	NodeID  string
	Replica int
}
type Snapshot struct {
	Generation     uint64
	UsedValueBytes int
	Nodes          []Node
	Tokens         []TokenView
}
