package resourceledger142

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

// policyConfig is immutable; replacement is a single atomic pointer swap.
type policyConfig struct {
	maxOps int
	actors map[string]struct{}
}

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	cfg atomic.Pointer[policyConfig]
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return nil, ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	p := &Policy{}
	p.cfg.Store(&policyConfig{maxOps: maxOps, actors: set})
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
	old := p.cfg.Load()
	maxOps := 0
	if old != nil {
		maxOps = old.maxOps
	}
	p.cfg.Store(&policyConfig{maxOps: maxOps, actors: set})
	return nil
}

func (p *Policy) Authorize(actor string, numOps int) error {
	cfg := p.cfg.Load()
	if cfg == nil {
		return ErrDenied
	}
	if numOps > cfg.maxOps {
		return ErrDenied
	}
	if _, ok := cfg.actors[actor]; !ok {
		return ErrDenied
	}
	return nil
}
