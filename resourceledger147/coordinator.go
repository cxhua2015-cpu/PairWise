package resourceledger147

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}
type Coordinator struct{}

func NewCoordinator(*Ledger, *Policy) (*Coordinator, error) { return nil, ErrNotImplemented }
func (*Coordinator) Apply(string, Batch) (Result, error)    { return Result{}, ErrNotImplemented }
func (*Coordinator) Decisions() []Decision                  { return nil }
