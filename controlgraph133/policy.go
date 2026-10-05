package controlgraph133

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration.
// The actor allow-list is replaced atomically as a whole; readers never
// observe a partially applied list.
type Policy struct {
	mu      sync.RWMutex
	maxOps  int
	actors  map[string]struct{}
	nameMax int
}

func NewPolicy(maxOps int, actors []string) (*Policy, error) {
	if maxOps <= 0 {
		return nil, ErrInvalidOptions
	}
	p := &Policy{maxOps: maxOps, nameMax: 256}
	if err := p.ReplaceActors(actors); err != nil {
		return nil, err
	}
	return p, nil
}

func validActor(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ReplaceActors atomically swaps the whole allow-list. The input slice is
// copied, so later caller mutations cannot affect the policy.
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

// Authorize reports nil when the actor is on the allow-list and opCount is
// within the per-request operation limit. It never touches core state.
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
