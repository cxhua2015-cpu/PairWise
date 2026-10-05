package resourceledger142

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
	p.mu.Lock()
	p.allowed = next
	p.mu.Unlock()
	return nil
}

func (p *Policy) Authorize(actor string, opCount int) error {
	if opCount > p.maxOps {
		return ErrDenied
	}
	p.mu.RLock()
	_, ok := p.allowed[actor]
	p.mu.RUnlock()
	if !ok {
		return ErrDenied
	}
	return nil
}
