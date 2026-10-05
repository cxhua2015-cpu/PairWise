package resourcelease149

import (
	"errors"
	"sync"
)

var ErrDenied = errors.New("admission denied")

// Policy owns the independently synchronized admission configuration:
// an atomically replaceable actor allow-list plus a per-batch op limit.
type Policy struct {
	mu     sync.RWMutex
	maxOps int
	actors map[string]struct{}
}

func validActor(name string) bool {
	if len(name) == 0 {
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

// ReplaceActors atomically swaps the allow-list; in-flight Authorize
// calls see either the old or the new list, never a mix.
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

// Authorize reports ErrDenied when the actor is not allowed or the
// requested op count exceeds the per-batch limit.
func (p *Policy) Authorize(actor string, numOps int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.actors[actor]; !ok {
		return ErrDenied
	}
	if numOps < 0 || numOps > p.maxOps {
		return ErrDenied
	}
	return nil
}
