package dagstore

import (
	"errors"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCycle          = errors.New("cycle")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxNodes, MaxEdges, MaxNameBytes, MaxPayloadBytes, MaxTotalPayloadBytes int }
type OpKind uint8

const (
	AddNode OpKind = iota + 1
	UpdateNode
	DeleteNode
	AddEdge
	RemoveEdge
)

type Op struct {
	Kind           OpKind
	Name, From, To string
	Payload        []byte
}
type Batch struct{ Ops []Op }
type Node struct {
	Name     string
	Payload  []byte
	Revision uint64
}
type Edge struct {
	From, To string
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	ChangedNodes         []Node
	ChangedEdges         []Edge
}
type Snapshot struct {
	Generation, NextRevision uint64
	Nodes                    []Node
	Edges                    []Edge
}
type Store struct {
	mu         sync.RWMutex
	opts       Options
	nodes      map[string]*nodeState
	edges      map[edgeKey]uint64
	out        map[string]map[string]struct{}
	in         map[string]map[string]struct{}
	generation uint64
	revision   uint64
}
