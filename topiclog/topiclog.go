package topiclog

import "errors"

var (
	ErrNotImplemented   = errors.New("topiclog: not implemented")
	ErrInvalidOptions   = errors.New("topiclog: invalid options")
	ErrInvalidName      = errors.New("topiclog: invalid name")
	ErrInvalidKey       = errors.New("topiclog: invalid key")
	ErrPayloadTooLarge  = errors.New("topiclog: payload too large")
	ErrInvalidPartition = errors.New("topiclog: invalid partition")
	ErrInvalidLimit     = errors.New("topiclog: invalid limit")
	ErrCapacity         = errors.New("topiclog: capacity exceeded")
	ErrOffsetRegression = errors.New("topiclog: offset regression")
	ErrOffsetAhead      = errors.New("topiclog: offset ahead")
	ErrNotFound         = errors.New("topiclog: not found")
	ErrUnsafeTrim       = errors.New("topiclog: unsafe trim")
)

type Options struct{ Partitions, MaxTopics, MaxRecords, MaxPayloadBytes int }
type Input struct {
	Topic, Key string
	Payload    []byte
}
type Record struct {
	Topic     string
	Partition int
	Offset    uint64
	Key       string
	Payload   []byte
}
type Commit struct {
	Group, Topic string
	Partition    int
	Offset       uint64
}
type ReadResult struct {
	Generation uint64
	Records    []Record
}
type PartitionView struct {
	Partition  int
	HighOffset uint64
	Records    []Record
}
type TopicView struct {
	Topic      string
	Partitions []PartitionView
}
type Snapshot struct {
	Generation                    uint64
	UsedRecords, UsedPayloadBytes int
	Topics                        []TopicView
	Commits                       []Commit
}
type Log struct{}

func New(Options) (*Log, error)                            { return nil, ErrNotImplemented }
func (*Log) AppendBatch([]Input) ([]Record, uint64, error) { return nil, 0, ErrNotImplemented }
func (*Log) Read(string, int, uint64, int) (ReadResult, error) {
	return ReadResult{}, ErrNotImplemented
}
func (*Log) CommitBatch([]Commit) (uint64, error)     { return 0, ErrNotImplemented }
func (*Log) Trim(string, int, uint64) (uint64, error) { return 0, ErrNotImplemented }
func (*Log) Snapshot() Snapshot                       { return Snapshot{} }
