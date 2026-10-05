package prefixacl

import (
	"errors"
	"net/netip"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Action uint8

const (
	Allow Action = iota + 1
	Deny
)

type Kind uint8

const (
	Upsert Kind = iota + 1
	Delete
)

type Options struct {
	MaxRules int
	Default  Action
}
type Op struct {
	Kind   Kind
	Prefix netip.Prefix
	Action Action
}
type Batch struct{ Ops []Op }
type Rule struct {
	Prefix   netip.Prefix
	Action   Action
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Rule
}
type Snapshot struct {
	Generation, NextRevision uint64
	Rules                    []Rule
}
type Table struct{}

func New(Options) (*Table, error)          { return nil, ErrNotImplemented }
func (*Table) Apply(Batch) (Result, error) { return Result{}, ErrNotImplemented }
func (*Table) Lookup(netip.Addr) (Action, netip.Prefix, bool, error) {
	return 0, netip.Prefix{}, false, ErrNotImplemented
}
func (*Table) Snapshot() Snapshot { return Snapshot{} }
