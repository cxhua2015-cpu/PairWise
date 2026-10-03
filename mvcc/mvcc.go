package mvcc

import (
	"errors"
	"sync"
)

var (
	ErrNotImplemented  = errors.New("mvcc: not implemented")
	ErrInvalidOptions  = errors.New("mvcc: invalid options")
	ErrInvalidKey      = errors.New("mvcc: invalid key")
	ErrInvalidRange    = errors.New("mvcc: invalid range")
	ErrInvalidRevision = errors.New("mvcc: invalid revision")
	ErrFutureRevision  = errors.New("mvcc: future revision")
	ErrCompacted       = errors.New("mvcc: compacted")
	ErrInvalidLimit    = errors.New("mvcc: invalid limit")
	ErrInvalidCompare  = errors.New("mvcc: invalid compare")
	ErrInvalidOp       = errors.New("mvcc: invalid operation")
	ErrCapacity        = errors.New("mvcc: capacity exceeded")
)

type Options struct{ MaxLiveBytes int }

type EventType uint8

const (
	EventPut EventType = iota + 1
	EventDelete
)

type KV struct {
	Key            string
	Value          []byte
	CreateRevision uint64
	ModRevision    uint64
	Version        uint64
}

type Event struct {
	Type     EventType
	KV       KV
	Prev     *KV
	Revision uint64
	Sequence uint32
}

type CompareTarget uint8

const (
	CompareExists CompareTarget = iota + 1
	CompareValue
	CompareModRevision
	CompareVersion
)

type CompareResult uint8

const (
	CompareEqual CompareResult = iota + 1
	CompareNotEqual
	CompareLess
	CompareGreater
)

type Compare struct {
	Key      string
	Target   CompareTarget
	Result   CompareResult
	Revision uint64
	Value    []byte
}

type OpType uint8

const (
	OpRange OpType = iota + 1
	OpPut
	OpDelete
)

type Op struct {
	Type     OpType
	Key      string
	End      string
	Value    []byte
	Revision uint64
}

type OpResponse struct {
	KVs     []KV
	Deleted int
}

type TxnResponse struct {
	Succeeded bool
	Revision  uint64
	Responses []OpResponse
	Events    []Event
}

type Snapshot struct {
	Revision        uint64
	CompactRevision uint64
	LiveBytes       int
	KVs             []KV
}

type Store struct {
	mu         sync.Mutex
	maxLive    int
	liveBytes  int
	revision   uint64
	compactRev uint64
	live       map[string]version
	history    map[string][]version
	events     []Event
}

func New(opts Options) (*Store, error) {
	if opts.MaxLiveBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		maxLive: opts.MaxLiveBytes,
		live:    make(map[string]version),
		history: make(map[string][]version),
	}, nil
}
