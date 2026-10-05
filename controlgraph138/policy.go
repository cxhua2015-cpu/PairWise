package controlgraph138

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
	if maxOps <= 0 || len(actors) == 0 {
		return nil, ErrInvalidInput
	}
	p := &Policy{maxOps: maxOps}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Policy) ReplaceActors(actors []string) error {
	if len(actors) == 0 {
		return ErrInvalidInput
	}
	next := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return ErrInvalidInput
		}
		next[a] = struct{}{}
	}
	p.mu.Lock()
	p.actors = next
	p.mu.Unlock()
	return nil
}

func (p *Policy) Authorize(actor string, numOps int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	if numOps > p.maxOps {
		return ErrDenied
	}
	return nil
}
