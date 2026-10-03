package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrNotImplemented   = errors.New("jsondoc: not implemented")
	ErrInvalidOptions   = errors.New("jsondoc: invalid options")
	ErrInvalidJSON      = errors.New("jsondoc: invalid json")
	ErrInvalidOperation = errors.New("jsondoc: invalid operation")
	ErrInvalidPointer   = errors.New("jsondoc: invalid pointer")
	ErrInvalidIndex     = errors.New("jsondoc: invalid array index")
	ErrNotFound         = errors.New("jsondoc: not found")
	ErrTypeMismatch     = errors.New("jsondoc: type mismatch")
	ErrRootRemoval      = errors.New("jsondoc: cannot remove root")
	ErrMoveIntoChild    = errors.New("jsondoc: move into child")
	ErrTestFailed       = errors.New("jsondoc: test failed")
	ErrRevisionConflict = errors.New("jsondoc: revision conflict")
	ErrCapacity         = errors.New("jsondoc: capacity exceeded")
)

type Options struct{ MaxNodes int }
type Operation struct {
	Op, Path, From string
	Value          json.RawMessage
}
type Result struct {
	Revision uint64
	Nodes    int
	Document []byte
}
type Snapshot struct {
	Revision uint64
	Nodes    int
	Document []byte
}
type OpError struct {
	Index int
	Path  string
	Err   error
}

func (e *OpError) Error() string {
	return fmt.Sprintf("jsondoc: op %d path %q: %v", e.Index, e.Path, e.Err)
}
func (e *OpError) Unwrap() error { return e.Err }

type Store struct{}

func New(initial []byte, opts Options) (*Store, error) { return nil, ErrNotImplemented }
func (s *Store) Apply(expectedRevision uint64, ops []Operation) (Result, error) {
	return Result{}, ErrNotImplemented
}
func (s *Store) Snapshot() Snapshot { return Snapshot{} }
