package controlgraph143

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
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return nil, ErrInvalidInput
		}
		for i := 0; i < len(a); i++ {
			if a[i] < 0x21 || a[i] > 0x7e {
				return nil, ErrInvalidInput
			}
		}
		set[a] = struct{}{}
	}
	return set, nil
}

// ReplaceActors atomically swaps the allow-list; on error the old list stays.
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

// Authorize checks the actor allow-list and the per-batch operation limit.
func (p *Policy) Authorize(actor string, opCount int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	if opCount > p.maxOps {
		return ErrDenied
	}
	return nil
}
