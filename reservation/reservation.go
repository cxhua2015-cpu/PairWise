package reservation

import "errors"

var (
	ErrNotImplemented  = errors.New("reservation: not implemented")
	ErrInvalidOptions  = errors.New("reservation: invalid options")
	ErrInvalidChange   = errors.New("reservation: invalid change")
	ErrInvalidID       = errors.New("reservation: invalid id")
	ErrInvalidResource = errors.New("reservation: invalid resource")
	ErrInvalidInterval = errors.New("reservation: invalid interval")
	ErrValueTooLarge   = errors.New("reservation: value too large")
	ErrDuplicate       = errors.New("reservation: duplicate")
	ErrNotFound        = errors.New("reservation: not found")
	ErrCapacity        = errors.New("reservation: capacity exceeded")
	ErrConflict        = errors.New("reservation: conflict")
	ErrInvalidScan     = errors.New("reservation: invalid scan")
)

type Options struct{ MaxResources, MaxReservations, MaxValueBytes int }
type Reservation struct {
	ID, Resource string
	Start, End   int64
	Value        []byte
}
type ChangeType uint8

const (
	ChangeAdd ChangeType = iota + 1
	ChangeDelete
)

type Change struct {
	Type        ChangeType
	Reservation Reservation
	ID          string
}
type AtResult struct {
	Found       bool
	Generation  uint64
	Reservation Reservation
}
type ScanResult struct {
	Generation   uint64
	Reservations []Reservation
}
type ResourceView struct {
	Resource     string
	Reservations []Reservation
}
type Snapshot struct {
	Generation                       uint64
	UsedReservations, UsedValueBytes int
	Resources                        []ResourceView
}
