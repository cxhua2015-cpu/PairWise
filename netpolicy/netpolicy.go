package netpolicy

import "errors"

var (
	ErrNotImplemented  = errors.New("netpolicy: not implemented")
	ErrInvalidOptions  = errors.New("netpolicy: invalid options")
	ErrInvalidChange   = errors.New("netpolicy: invalid change")
	ErrInvalidID       = errors.New("netpolicy: invalid id")
	ErrInvalidPrefix   = errors.New("netpolicy: invalid prefix")
	ErrInvalidProtocol = errors.New("netpolicy: invalid protocol")
	ErrInvalidPort     = errors.New("netpolicy: invalid port range")
	ErrInvalidPriority = errors.New("netpolicy: invalid priority")
	ErrInvalidAction   = errors.New("netpolicy: invalid action")
	ErrValueTooLarge   = errors.New("netpolicy: value too large")
	ErrDuplicateID     = errors.New("netpolicy: duplicate id")
	ErrNotFound        = errors.New("netpolicy: rule not found")
	ErrCapacity        = errors.New("netpolicy: capacity exceeded")
	ErrInvalidAddress  = errors.New("netpolicy: invalid address")
)

type Protocol uint8

const (
	ProtocolAny Protocol = iota
	ProtocolTCP
	ProtocolUDP
)

type Action uint8

const (
	ActionAllow Action = iota
	ActionDeny
)

type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeDelete
)

type Options struct{ MaxRules, MaxValueBytes int }
type Rule struct {
	ID, Prefix         string
	Protocol           Protocol
	PortStart, PortEnd uint16
	Priority           int
	Action             Action
	Value              []byte
}
type Change struct {
	Type ChangeType
	Rule Rule
	ID   string
}
type Decision struct {
	Found          bool
	Action         Action
	RuleID, Prefix string
	Protocol       Protocol
	Value          []byte
	Generation     uint64
}
type RuleView struct {
	ID, Prefix         string
	Protocol           Protocol
	PortStart, PortEnd uint16
	Priority           int
	Action             Action
	Value              []byte
}
type Snapshot struct {
	Generation     uint64
	UsedValueBytes int
	Rules          []RuleView
}
// Table is implemented in table.go.
