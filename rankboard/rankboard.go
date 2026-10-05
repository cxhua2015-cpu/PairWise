package rankboard

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrScore          = errors.New("invalid score")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Add Kind = iota + 1
	Set
	Delete
)

type Options struct {
	MaxItems, MaxIDBytes int
	MaxAbsScore          int64
}
type Op struct {
	Kind         Kind
	ID           string
	Delta, Score int64
}
type Batch struct{ Ops []Op }
type Item struct {
	ID       string
	Score    int64
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Item
}
type Snapshot struct {
	Generation, NextRevision uint64
	Items                    []Item
}
type Board struct{}

func New(Options) (*Board, error)          { return nil, ErrNotImplemented }
func (*Board) Apply(Batch) (Result, error) { return Result{}, ErrNotImplemented }
func (*Board) Top(int) ([]Item, error)     { return nil, ErrNotImplemented }
func (*Board) Snapshot() Snapshot          { return Snapshot{} }
