package resourcecatalog141

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	mu     sync.RWMutex
	maxOps int
	actors map[string]struct{}
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	set, err := actorSet(actors)
	if err != nil {
		return nil, err
	}
	return &Policy{maxOps: maxOps, actors: set}, nil
}

func actorSet(actors []string) (map[string]struct{}, error) {
	if len(actors) == 0 {
		return nil, ErrInvalidInput
	}
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return nil, ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	return set, nil
}

func (p *Policy) ReplaceActors(actors []string) error {
	set, err := actorSet(actors)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.actors = set
	p.mu.Unlock()
	return nil
}

func (p *Policy) Authorize(actor string, ops int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	if ops > p.maxOps {
		return ErrDenied
	}
	return nil
}
