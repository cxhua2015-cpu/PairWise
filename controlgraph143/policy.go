package controlgraph143

import "errors"

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
type Policy struct{}

func NewPolicy(int, []string) (*Policy, error) { return nil, ErrNotImplemented }
func (*Policy) ReplaceActors([]string) error   { return ErrNotImplemented }
func (*Policy) Authorize(string, int) error    { return ErrNotImplemented }
