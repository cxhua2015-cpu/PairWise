package hashring

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	maxIDLen      = 64
	maxWeight     = 128
	maxValueLen   = 1 << 20
	limitNodes    = 10000
	limitTokens   = 1000000
	limitValueSum = 64 << 20
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

func defaultHasher(b []byte) uint64 {
	sum := sha256.Sum256(b)
	return binary.BigEndian.Uint64(sum[:8])
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > maxIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validWeight(w int) bool { return w >= 1 && w <= maxWeight }

func validateChange(c Change) error {
	switch c.Type {
	case ChangeAdd:
		if c.ID != "" {
			return fmt.Errorf("%w: add must not set Change.ID", ErrInvalidChange)
		}
		if !validID(c.Node.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.Node.ID)
		}
		if !validWeight(c.Node.Weight) {
			return fmt.Errorf("%w: %d", ErrInvalidWeight, c.Node.Weight)
		}
		if len(c.Node.Value) > maxValueLen {
			return fmt.Errorf("%w: %d bytes", ErrValueTooLarge, len(c.Node.Value))
		}
	case ChangeRemove:
		if c.Node.ID != "" {
			return fmt.Errorf("%w: remove must not set Change.Node.ID", ErrInvalidChange)
		}
		if !validID(c.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.ID)
		}
	case ChangeUpdate:
		if c.Node.ID != "" {
			return fmt.Errorf("%w: update must not set Change.Node.ID", ErrInvalidChange)
		}
		if !validID(c.ID) {
			return fmt.Errorf("%w: %q", ErrInvalidID, c.ID)
		}
		if !validWeight(c.Node.Weight) {
			return fmt.Errorf("%w: %d", ErrInvalidWeight, c.Node.Weight)
		}
		if len(c.Node.Value) > maxValueLen {
			return fmt.Errorf("%w: %d bytes", ErrValueTooLarge, len(c.Node.Value))
		}
	default:
		return fmt.Errorf("%w: unknown type %d", ErrInvalidChange, c.Type)
	}
	return nil
}

func New(opts Options) (*Ring, error) {
	if opts.MaxNodes < 1 || opts.MaxNodes > limitNodes ||
		opts.MaxTokens < 1 || opts.MaxTokens > limitTokens ||
		opts.MaxValueBytes < 1 || opts.MaxValueBytes > limitValueSum {
		return nil, fmt.Errorf("%w: %+v", ErrInvalidOptions, opts)
	}
	h := opts.Hasher
	if h == nil {
		h = defaultHasher
	}
	return &Ring{
		hasher:        h,
		maxNodes:      opts.MaxNodes,
		maxTokens:     opts.MaxTokens,
		maxValueBytes: opts.MaxValueBytes,
		nodes:         make(map[string]*nodeState),
	}, nil
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
	candidate := make(map[string]*nodeState, len(r.nodes))
	for id, n := range r.nodes {
		candidate[id] = n
	}
	for _, c := range changes {
		switch c.Type {
		case ChangeAdd:
			if _, ok := candidate[c.Node.ID]; ok {
				return r.generation, fmt.Errorf("%w: %q", ErrDuplicate, c.Node.ID)
			}
			candidate[c.Node.ID] = &nodeState{
				weight: c.Node.Weight,
				value:  append([]byte(nil), c.Node.Value...),
			}
		case ChangeRemove:
			if _, ok := candidate[c.ID]; !ok {
				return r.generation, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
			}
			delete(candidate, c.ID)
		case ChangeUpdate:
			if _, ok := candidate[c.ID]; !ok {
				return r.generation, fmt.Errorf("%w: %q", ErrNotFound, c.ID)
			}
			candidate[c.ID] = &nodeState{
				weight: c.Node.Weight,
				value:  append([]byte(nil), c.Node.Value...),
			}
		}
	}
	totalTokens, totalBytes := 0, 0
	for _, n := range candidate {
		totalTokens += n.weight
		totalBytes += len(n.value)
	}
	if len(candidate) > r.maxNodes || totalTokens > r.maxTokens || totalBytes > r.maxValueBytes {
		return r.generation, fmt.Errorf("%w: nodes=%d tokens=%d valueBytes=%d",
			ErrCapacity, len(candidate), totalTokens, totalBytes)
	}
	r.nodes = candidate
	r.usedValueBytes = totalBytes
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
			tokens = append(tokens, token{
				hash:    r.hasher([]byte(input)),
				nodeID:  id,
				replica: replica,
			})
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
	if len(key) == 0 || count < 1 || count > r.maxNodes {
		return nil, 0, fmt.Errorf("%w: keyLen=%d count=%d", ErrInvalidLookup, len(key), count)
	}
	h := r.hasher(key)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tokens) == 0 {
		return nil, r.generation, ErrEmpty
	}
	start := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i].hash >= h })
	if start == len(r.tokens) {
		start = 0
	}
	want := count
	if len(r.nodes) < want {
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
		owners = append(owners, Owner{
			ID:    t.nodeID,
			Value: append([]byte(nil), r.nodes[t.nodeID].value...),
		})
	}
	return owners, r.generation, nil
}

func (r *Ring) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := Snapshot{
		Generation:     r.generation,
		UsedValueBytes: r.usedValueBytes,
		Nodes:          make([]Node, 0, len(r.nodes)),
		Tokens:         make([]TokenView, len(r.tokens)),
	}
	for id, n := range r.nodes {
		snap.Nodes = append(snap.Nodes, Node{
			ID:     id,
			Weight: n.weight,
			Value:  append([]byte(nil), n.value...),
		})
	}
	sort.Slice(snap.Nodes, func(i, j int) bool {
		return strings.Compare(snap.Nodes[i].ID, snap.Nodes[j].ID) < 0
	})
	for i, t := range r.tokens {
		snap.Tokens[i] = TokenView{Hash: t.hash, NodeID: t.nodeID, Replica: t.replica}
	}
	return snap
}
