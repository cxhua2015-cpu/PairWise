package resourceledger132

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	maxOps int
	actors atomic.Value // map[string]struct{}
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	p := &Policy{maxOps: maxOps}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Policy) ReplaceActors(actors []string) error {
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	p.actors.Store(set)
	return nil
}

func (p *Policy) Authorize(actor string, opCount int) error {
	if opCount < 0 || opCount > p.maxOps {
		return ErrDenied
	}
	set, _ := p.actors.Load().(map[string]struct{})
	if _, ok := set[actor]; !ok {
		return ErrDenied
	}
	return nil
}
