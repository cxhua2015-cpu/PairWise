package ownershipgraph

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrCycle          = errors.New("cycle")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	AddNode Kind = iota + 1
	DeleteNode
	AddEdge
	DeleteEdge
)

type Options struct{ MaxNodes, MaxEdges, MaxNameBytes int }
type Op struct {
	Kind     Kind
	From, To string
}
type Batch struct{ Ops []Op }
type Edge struct{ From, To string }
type Result struct{ Generation uint64 }
type Snapshot struct {
	Generation uint64
	Nodes      []string
	Edges      []Edge
}
type Graph struct{}

func New(Options) (*Graph, error)                     { return nil, ErrNotImplemented }
func (*Graph) Apply(Batch) (Result, error)            { return Result{}, ErrNotImplemented }
func (*Graph) Reachable(string, string) (bool, error) { return false, ErrNotImplemented }
func (*Graph) Snapshot() Snapshot                     { return Snapshot{} }
