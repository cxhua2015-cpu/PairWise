package hashring

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"sync"
)

const (
	maxNodesLimit       = 10000
	maxTokensLimit      = 1000000
	maxValueBytesLimit  = 64 << 20
	maxSingleValueBytes = 1 << 20
	maxWeight           = 128
)

type nodeState struct {
	weight int
	value  []byte
}

type token struct {
	hash    uint64
	nodeID  string
	replica int
}

// Ring is a concurrency-safe weighted consistent hash ring.
type Ring struct {
	mu             sync.RWMutex
	maxNodes       int
	maxTokens      int
	maxValueBytes  int
	hasher         Hasher
	nodes          map[string]nodeState
	tokens         []token
	generation     uint64
	usedValueBytes int
}

func defaultHasher(b []byte) uint64 {
	sum := sha256.Sum256(b)
	return binary.BigEndian.Uint64(sum[:8])
}

func New(opts Options) (*Ring, error) {
	if opts.MaxNodes < 1 || opts.MaxNodes > maxNodesLimit ||
		opts.MaxTokens < 1 || opts.MaxTokens > maxTokensLimit ||
		opts.MaxValueBytes < 1 || opts.MaxValueBytes > maxValueBytesLimit {
		return nil, fmt.Errorf("%w: MaxNodes=%d MaxTokens=%d MaxValueBytes=%d",
			ErrInvalidOptions, opts.MaxNodes, opts.MaxTokens, opts.MaxValueBytes)
	}
	h := opts.Hasher
	if h == nil {
		h = defaultHasher
	}
	return &Ring{
		maxNodes:      opts.MaxNodes,
		maxTokens:     opts.MaxTokens,
		maxValueBytes: opts.MaxValueBytes,
		hasher:        h,
		nodes:         make(map[string]nodeState),
	}, nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func validateChange(c Change) error {
	switch c.Type {
	case ChangeAdd:
		if c.ID != "" {
			return fmt.Errorf("%w: add requires empty Change.ID", ErrInvalidChange)
		}
		if !validID(c.Node.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.Node.ID)
		}
		if err := validateWeightValue(c.Node); err != nil {
			return err
		}
	case ChangeRemove:
		if c.Node.ID != "" {
			return fmt.Errorf("%w: remove requires empty Node.ID", ErrInvalidChange)
		}
		if !validID(c.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.ID)
		}
	case ChangeUpdate:
		if c.Node.ID != "" {
			return fmt.Errorf("%w: update requires empty Node.ID", ErrInvalidChange)
		}
		if !validID(c.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.ID)
		}
		if err := validateWeightValue(c.Node); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidChange, c.Type)
	}
	return nil
}

func validateWeightValue(n Node) error {
	if n.Weight < 1 || n.Weight > maxWeight {
		return fmt.Errorf("%w: %d", ErrInvalidWeight, n.Weight)
	}
	if len(n.Value) > maxSingleValueBytes {
		return fmt.Errorf("%w: %d bytes", ErrValueTooLarge, len(n.Value))
	}
	return nil
}

func (r *Ring) Apply(c Change) (uint64, error) {
	return r.ApplyBatch([]Change{c})
}

func (r *Ring) ApplyBatch(changes []Change) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(changes) == 0 {
		return r.generation, nil
	}
	for _, c := range changes {
		if err := validateChange(c); err != nil {
			return r.generation, err
		}
	}
	candidate := make(map[string]nodeState, len(r.nodes)+len(changes))
	for id, n := range r.nodes {
		candidate[id] = n
	}
	for _, c := range changes {
		switch c.Type {
		case ChangeAdd:
			id := c.Node.ID
			if _, ok := candidate[id]; ok {
				return r.generation, fmt.Errorf("%w: %q", ErrDuplicate, id)
			}
			candidate[id] = nodeState{weight: c.Node.Weight, value: cloneBytes(c.Node.Value)}
		case ChangeRemove:
			if _, ok := candidate[c.ID]; !ok {
				return r.generation, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
			}
			delete(candidate, c.ID)
		case ChangeUpdate:
			if _, ok := candidate[c.ID]; !ok {
				return r.generation, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
			}
			candidate[c.ID] = nodeState{weight: c.Node.Weight, value: cloneBytes(c.Node.Value)}
		}
	}
	totalTokens := 0
	totalValue := 0
	for _, n := range candidate {
		totalTokens += n.weight
		totalValue += len(n.value)
	}
	switch {
	case len(candidate) > r.maxNodes:
		return r.generation, fmt.Errorf("%w: %d nodes > %d", ErrCapacity, len(candidate), r.maxNodes)
	case totalTokens > r.maxTokens:
		return r.generation, fmt.Errorf("%w: %d tokens > %d", ErrCapacity, totalTokens, r.maxTokens)
	case totalValue > r.maxValueBytes:
		return r.generation, fmt.Errorf("%w: %d value bytes > %d", ErrCapacity, totalValue, r.maxValueBytes)
	}
	r.nodes = candidate
	r.usedValueBytes = totalValue
	r.rebuildTokensLocked()
	r.generation++
	return r.generation, nil
}

func (r *Ring) rebuildTokensLocked() {
	total := 0
	for _, n := range r.nodes {
		total += n.weight
	}
	tokens := make([]token, 0, total)
	for id, n := range r.nodes {
		for replica := 0; replica < n.weight; replica++ {
			input := id + "#" + strconv.Itoa(replica)
			tokens = append(tokens, token{hash: r.hasher([]byte(input)), nodeID: id, replica: replica})
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		a, b := tokens[i], tokens[j]
		if a.hash != b.hash {
			return a.hash < b.hash
		}
		if a.nodeID != b.nodeID {
			return a.nodeID < b.nodeID
		}
		return a.replica < b.replica
	})
	r.tokens = tokens
}

func (r *Ring) Lookup(key []byte, count int) ([]Owner, uint64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(key) == 0 || count < 1 || count > r.maxNodes {
		return nil, r.generation, fmt.Errorf("%w: key=%d bytes count=%d", ErrInvalidLookup, len(key), count)
	}
	if len(r.tokens) == 0 {
		return nil, r.generation, ErrEmpty
	}
	h := r.hasher(key)
	start := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i].hash >= h })
	if start == len(r.tokens) {
		start = 0
	}
	want := count
	if want > len(r.nodes) {
		want = len(r.nodes)
	}
	owners := make([]Owner, 0, want)
	seen := make(map[string]struct{}, want)
	for i := 0; i < len(r.tokens) && len(owners) < want; i++ {
		t := r.tokens[(start+i)%len(r.tokens)]
		if _, ok := seen[t.nodeID]; ok {
			continue
		}
		seen[t.nodeID] = struct{}{}
		owners = append(owners, Owner{ID: t.nodeID, Value: cloneBytes(r.nodes[t.nodeID].value)})
	}
	return owners, r.generation, nil
}

func (r *Ring) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make([]Node, 0, len(r.nodes))
	for id, n := range r.nodes {
		nodes = append(nodes, Node{ID: id, Weight: n.weight, Value: cloneBytes(n.value)})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	tokens := make([]TokenView, len(r.tokens))
	for i, t := range r.tokens {
		tokens[i] = TokenView{Hash: t.hash, NodeID: t.nodeID, Replica: t.replica}
	}
	return Snapshot{
		Generation:     r.generation,
		UsedValueBytes: r.usedValueBytes,
		Nodes:          nodes,
		Tokens:         tokens,
	}
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return bytes.Clone(b)
}
