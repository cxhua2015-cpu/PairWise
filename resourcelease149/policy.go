package resourcelease149

import (
	"errors"
	"sync/atomic"
)

var ErrDenied = errors.New("admission denied")

type policyConfig struct {
	maxOps int
	actors map[string]struct{}
}

// Policy owns the independently synchronized admission configuration.
type Policy struct {
	cfg atomic.Pointer[policyConfig]
}

func validActor(a string) bool {
	if a == "" {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	p := &Policy{}
	cfg, err := buildConfig(maxOps, actors)
	if err != nil {
		return nil, err
	}
	p.cfg.Store(cfg)
	return p, nil
}

func buildConfig(maxOps int, actors []string) (*policyConfig, error) {
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if !validActor(a) {
			return nil, ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	return &policyConfig{maxOps: maxOps, actors: set}, nil
}

func (p *Policy) ReplaceActors(actors []string) error {
	cur := p.cfg.Load()
	cfg, err := buildConfig(cur.maxOps, actors)
	if err != nil {
		return err
	}
	p.cfg.Store(cfg)
	return nil
}

func (p *Policy) Authorize(actor string, opCount int) error {
	cfg := p.cfg.Load()
	if _, ok := cfg.actors[actor]; !ok {
		return ErrDenied
	}
	if opCount > cfg.maxOps {
		return ErrDenied
	}
	return nil
}
