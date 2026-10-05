package taskqueue140

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration:
// an atomically replaceable actor allow-list plus a per-batch operation
// limit. It never touches core queue state.
type Policy struct {
	mu     sync.RWMutex
	maxOps int
	actors map[string]struct{}
}

func validActor(a string) bool {
	if a == "" {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
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
	set, err := actorSet(actors)
	if err != nil {
		return nil, err
	}
	return &Policy{maxOps: maxOps, actors: set}, nil
}

func actorSet(actors []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if !validActor(a) {
			return nil, ErrInvalidInput
		}
		set[a] = struct{}{}
	}
	return set, nil
}

// ReplaceActors atomically swaps the allow-list.
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

// Authorize reports nil when the actor is allowed and the operation count
// fits the per-batch limit, otherwise ErrDenied.
func (p *Policy) Authorize(actor string, opCount int) error {
	if opCount < 0 {
		return ErrDenied
	}
	p.mu.RLock()
	_, ok := p.actors[actor]
	maxOps := p.maxOps
	p.mu.RUnlock()
	if !ok || opCount > maxOps {
		return ErrDenied
	}
	return nil
}
