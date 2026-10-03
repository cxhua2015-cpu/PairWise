package idempotency

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidKey     = errors.New("invalid key")
	ErrInvalidTime    = errors.New("invalid time")
	ErrInvalidToken   = errors.New("invalid token")
	ErrResultTooLarge = errors.New("result too large")
	ErrConflict       = errors.New("fingerprint conflict")
	ErrStaleToken     = errors.New("stale token")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	MaxEntries     int
	MaxResultBytes int
	MaxKeyBytes    int
}

type BeginResult struct {
	Leader     bool
	Pending    bool
	Replay     bool
	Token      uint64
	LeaseUntil int64
	Result     []byte
}

type Record struct {
	Key         string
	Fingerprint string
	Pending     bool
	Token       uint64
	LeaseUntil  int64
	ReplayUntil int64
	Result      []byte
}

type Snapshot struct {
	Generation  uint64
	NextToken   uint64
	Entries     int
	ResultBytes int
	Records     []Record
}

type Registry struct{}

func New(opts Options) (*Registry, error) { return nil, ErrInvalidOptions }
func (r *Registry) Begin(key, fingerprint string, now, lease int64) (BeginResult, error) {
	return BeginResult{}, ErrStaleToken
}
func (r *Registry) Renew(key string, token uint64, now, lease int64) error { return ErrStaleToken }
func (r *Registry) Complete(key string, token uint64, result []byte, now, replayTTL int64) error {
	return ErrStaleToken
}
func (r *Registry) Abort(key string, token uint64) error         { return ErrStaleToken }
func (r *Registry) Sweep(now int64, limit int) ([]string, error) { return nil, nil }
func (r *Registry) Snapshot() Snapshot                           { return Snapshot{} }
