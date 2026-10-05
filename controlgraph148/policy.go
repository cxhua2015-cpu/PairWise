package controlgraph148

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
// The actor allow-list is replaced atomically; readers never block writers.
type Policy struct {
	maxOps int
	actors atomic.Value // map[string]struct{}, treated as immutable
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
	next := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return ErrInvalidInput
		}
		next[a] = struct{}{}
	}
	p.actors.Store(next)
	return nil
}

func (p *Policy) Authorize(actor string, numOps int) error {
	if numOps > p.maxOps {
		return ErrDenied
	}
	allowed, _ := p.actors.Load().(map[string]struct{})
	if _, ok := allowed[actor]; !ok {
		return ErrDenied
	}
	return nil
}
