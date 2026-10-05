package controlgraph148

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

func validActorSet(actors []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if a == "" {
			return nil, ErrInvalidInput
		}
		for i := 0; i < len(a); i++ {
			c := a[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return nil, ErrInvalidInput
			}
		}
		set[a] = struct{}{}
	}
	return set, nil
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	set, err := validActorSet(actors)
	if err != nil {
		return nil, err
	}
	return &Policy{maxOps: maxOps, actors: set}, nil
}

// ReplaceActors atomically swaps the actor allow-list.
func (p *Policy) ReplaceActors(actors []string) error {
	set, err := validActorSet(actors)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.actors = set
	p.mu.Unlock()
	return nil
}

// Authorize checks the actor allow-list and the per-batch operation limit.
// It never touches core graph state.
func (p *Policy) Authorize(actor string, opCount int) error {
	if opCount < 0 {
		return ErrInvalidInput
	}
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
