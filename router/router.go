package router

import "errors"

var (
	ErrNotImplemented = errors.New("router: not implemented")
	ErrInvalidOptions = errors.New("router: invalid options")
	ErrInvalidMethod  = errors.New("router: invalid method")
	ErrInvalidPattern = errors.New("router: invalid pattern")
	ErrInvalidPath    = errors.New("router: invalid path")
	ErrInvalidChange  = errors.New("router: invalid change")
	ErrInvalidParams  = errors.New("router: invalid params")
	ErrConflict       = errors.New("router: conflict")
	ErrNotFound       = errors.New("router: not found")
	ErrCapacity       = errors.New("router: capacity exceeded")
)

type Options struct{ MaxRoutes int }
type Route struct {
	Method, Pattern, Name string
	Value                 []byte
}
type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeRemove
)

type Change struct {
	Type  ChangeType
	Route Route
	Name  string
}
type UpdateResult struct {
	Generation uint64
	Routes     int
}
type Param struct{ Name, Value string }
type MatchResult struct {
	Found            bool
	MethodNotAllowed bool
	HeadFallback     bool
	RouteName        string
	Value            []byte
	Params           []Param
	Allowed          []string
	Generation       uint64
}
type Snapshot struct {
	Generation uint64
	Routes     []Route
}
type Router struct{}

func New(opts Options) (*Router, error)                     { return nil, ErrNotImplemented }
func (r *Router) Apply(change Change) (UpdateResult, error) { return UpdateResult{}, ErrNotImplemented }
func (r *Router) ApplyBatch(changes []Change) (UpdateResult, error) {
	return UpdateResult{}, ErrNotImplemented
}
func (r *Router) Match(method, escapedPath string) (MatchResult, error) {
	return MatchResult{}, ErrNotImplemented
}
func (r *Router) Build(name string, params map[string]string) (string, error) {
	return "", ErrNotImplemented
}
func (r *Router) Snapshot() Snapshot { return Snapshot{} }
