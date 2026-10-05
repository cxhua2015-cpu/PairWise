package taskqueue150

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

type actorSet struct {
	actors map[string]struct{}
}

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	maxOps int
	set    atomic.Value // actorSet
}

func validateActors(actors []string) error {
	seen := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return ErrInvalidInput
		}
		if _, ok := seen[a]; ok {
			return ErrInvalidInput
		}
		seen[a] = struct{}{}
	}
	return nil
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	if err := validateActors(actors); err != nil {
		return nil, err
	}
	p := &Policy{maxOps: maxOps}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Policy) ReplaceActors(actors []string) error {
	if err := validateActors(actors); err != nil {
		return err
	}
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		set[a] = struct{}{}
	}
	p.set.Store(actorSet{actors: set})
	return nil
}

func (p *Policy) Authorize(actor string, opCount int) error {
	if opCount < 0 || opCount > p.maxOps {
		return ErrDenied
	}
	set := p.set.Load().(actorSet)
	if _, ok := set.actors[actor]; !ok {
		return ErrDenied
	}
	return nil
}
