package resourcelease134

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
	p.mu.Lock()
	p.actors = set
	p.mu.Unlock()
	return nil
}

func (p *Policy) Authorize(actor string, ops int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if ops < 0 || ops > p.maxOps {
		return ErrDenied
	}
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	return nil
}
