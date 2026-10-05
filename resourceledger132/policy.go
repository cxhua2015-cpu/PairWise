package resourceledger132

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
	p := &Policy{maxOps: maxOps}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

func validActor(actor string) bool {
	if actor == "" {
		return false
	}
	for i := 0; i < len(actor); i++ {
		c := actor[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ReplaceActors atomically swaps the whole allow-list.
func (p *Policy) ReplaceActors(actors []string) error {
	next := make(map[string]struct{}, len(actors))
	for _, a := range actors {
		if !validActor(a) {
			return ErrInvalidInput
		}
		next[a] = struct{}{}
	}
	p.mu.Lock()
	p.actors = next
	p.mu.Unlock()
	return nil
}

// Authorize checks the actor allow-list and the per-batch operation limit.
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
