package taskqueue150

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration:
// an atomically replaceable actor allow-list and a per-batch operation limit.
type Policy struct {
	mu     sync.RWMutex
	maxOps int
	actors map[string]struct{}
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidInput
	}
	p := &Policy{maxOps: maxOps}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

// ReplaceActors atomically swaps the whole allow-list.
func (p *Policy) ReplaceActors(actors []string) error {
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

// Authorize checks the actor against the allow-list and the op count
// against the per-batch limit. It never touches core queue state.
func (p *Policy) Authorize(actor string, ops int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	if ops > p.maxOps {
		return ErrDenied
	}
	return nil
}
