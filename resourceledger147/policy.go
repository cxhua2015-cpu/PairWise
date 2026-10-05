package resourceledger147

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	mu      sync.RWMutex
	maxOps  int
	allowed map[string]struct{}
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidInput
	}
	p := &Policy{maxOps: maxOps, allowed: make(map[string]struct{}, len(actors))}
	for _, a := range actors {
		if a == "" {
			return nil, ErrInvalidInput
		}
		p.allowed[a] = struct{}{}
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
	p.mu.Lock()
	p.allowed = next
	p.mu.Unlock()
	return nil
}

func (p *Policy) Authorize(actor string, ops int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.allowed[actor]; !ok {
		return ErrDenied
	}
	if ops < 0 || ops > p.maxOps {
		return ErrDenied
	}
	return nil
}
