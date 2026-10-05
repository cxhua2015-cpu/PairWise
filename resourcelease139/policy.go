package resourcelease139

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
// The active configuration is swapped atomically, so Authorize never
// observes a partially replaced allow-list.
type Policy struct {
	cfg atomic.Pointer[policyConfig]
}

func validActor(actor string) bool {
	if actor == "" {
		return false
	}
	for i := 0; i < len(actor); i++ {
		c := actor[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func buildConfig(maxOps int, actors []string) (*policyConfig, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidInput
	}
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if !validActor(a) {
			return nil, ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	return &policyConfig{maxOps: maxOps, actors: set}, nil
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	cfg, err := buildConfig(maxOps, actors)
	if err != nil {
		return nil, err
	}
	p := &Policy{}
	p.cfg.Store(cfg)
	return p, nil
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
	if opCount < 0 || opCount > cfg.maxOps {
		return ErrDenied
	}
	if _, ok := cfg.actors[actor]; !ok {
		return ErrDenied
	}
	return nil
}
