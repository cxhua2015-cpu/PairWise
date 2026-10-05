package resourcecatalog146

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

// actorSet is an immutable allow-list snapshot swapped atomically.
type actorSet map[string]struct{}

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	maxOps int
	actors atomic.Pointer[actorSet]
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
	set := make(actorSet, len(actors))
	for _, a := range actors {
		if a == "" {
			return ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	p.actors.Store(&set)
	return nil
}

func (p *Policy) Authorize(actor string, numOps int) error {
	if numOps > p.maxOps {
		return ErrDenied
	}
	set := p.actors.Load()
	if set == nil {
		return ErrDenied
	}
	if _, ok := (*set)[actor]; !ok {
		return ErrDenied
	}
	return nil
}
