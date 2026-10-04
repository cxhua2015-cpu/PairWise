package prefixclaim

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrOwner          = errors.New("owner mismatch")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxClaims, MaxPathBytes, MaxOwnerBytes int }

type OpKind uint8

const (
	Claim OpKind = iota + 1
	Release
)

type Op struct {
	Kind        OpKind
	Path, Owner string
}

type Batch struct{ Ops []Op }

type Entry struct {
	Path, Owner string
	Revision    uint64
}

type Result struct {
	Generation, Revision uint64
	Changed              []Entry
}

type Snapshot struct {
	Generation, NextRevision uint64
	Entries                  []Entry
}

type Registry struct {
	mu            sync.RWMutex
	maxClaims     int
	maxPathBytes  int
	maxOwnerBytes int
	claims        map[string]Entry
	generation    uint64
	nextRevision  uint64
}

func New(o Options) (*Registry, error) {
	if o.MaxClaims <= 0 || o.MaxPathBytes <= 0 || o.MaxOwnerBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		maxClaims:     o.MaxClaims,
		maxPathBytes:  o.MaxPathBytes,
		maxOwnerBytes: o.MaxOwnerBytes,
		claims:        make(map[string]Entry),
		nextRevision:  1,
	}, nil
}

func isOwnerChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '/' || c == '-'
}

func isSegmentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '-'
}

func (r *Registry) validPath(p string) bool {
	if p == "" || len(p) > r.maxPathBytes || p[0] != '/' {
		return false
	}
	if p == "/" {
		return true
	}
	if p[len(p)-1] == '/' {
		return false
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			if !isSegmentChar(seg[i]) {
				return false
			}
		}
	}
	return true
}

func (r *Registry) validOwner(o string) bool {
	if o == "" || len(o) > r.maxOwnerBytes {
		return false
	}
	for i := 0; i < len(o); i++ {
		if !isOwnerChar(o[i]) {
			return false
		}
	}
	return true
}

func (r *Registry) validateOp(op Op) bool {
	switch op.Kind {
	case Claim, Release:
		return r.validPath(op.Path) && r.validOwner(op.Owner)
	default:
		return false
	}
}

// isPrefixPath reports whether ancestor is a path prefix of p on segment
// boundaries (ancestor == "/" prefixes everything).
func isPrefixPath(ancestor, p string) bool {
	if ancestor == "/" {
		return true
	}
	return strings.HasPrefix(p, ancestor+"/")
}

func (r *Registry) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if !r.validateOp(op) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	candidate := make(map[string]Entry, len(r.claims)+len(b.Ops))
	for p, e := range r.claims {
		candidate[p] = e
	}
	nextRev := r.nextRevision
	lastRev := r.nextRevision - 1
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Claim:
			conflict := false
			// Ancestor or equal paths of op.Path.
			for cur := op.Path; ; {
				if _, ok := candidate[cur]; ok {
					conflict = true
					break
				}
				if cur == "/" {
					break
				}
				idx := strings.LastIndex(cur, "/")
				if idx == 0 {
					cur = "/"
				} else {
					cur = cur[:idx]
				}
			}
			// Descendant paths of op.Path.
			if !conflict {
				for p := range candidate {
					if isPrefixPath(op.Path, p) {
						conflict = true
						break
					}
				}
			}
			if conflict {
				return Result{}, ErrConflict
			}
			candidate[op.Path] = Entry{Path: op.Path, Owner: op.Owner, Revision: nextRev}
			lastRev = nextRev
			nextRev++
			touched[op.Path] = struct{}{}
		case Release:
			e, ok := candidate[op.Path]
			if !ok {
				return Result{}, ErrNotFound
			}
			if e.Owner != op.Owner {
				return Result{}, ErrOwner
			}
			delete(candidate, op.Path)
		}
	}

	if len(candidate) > r.maxClaims {
		return Result{}, ErrCapacity
	}

	r.claims = candidate
	r.nextRevision = nextRev
	if len(b.Ops) > 0 {
		r.generation++
	}

	changed := make([]Entry, 0, len(touched))
	for p := range touched {
		if e, ok := r.claims[p]; ok {
			changed = append(changed, e)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })

	return Result{Generation: r.generation, Revision: lastRev, Changed: changed}, nil
}

func (r *Registry) Lookup(path string) (Entry, bool, error) {
	if !r.validPath(path) {
		return Entry{}, false, ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for cur := path; ; {
		if e, ok := r.claims[cur]; ok {
			return e, true, nil
		}
		if cur == "/" {
			return Entry{}, false, nil
		}
		idx := strings.LastIndex(cur, "/")
		if idx == 0 {
			cur = "/"
		} else {
			cur = cur[:idx]
		}
	}
}

func (r *Registry) Descendants(path, after string, limit int) ([]Entry, error) {
	if !r.validPath(path) {
		return nil, ErrInvalidInput
	}
	if after != "" && !r.validPath(after) {
		return nil, ErrInvalidInput
	}
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, limit)
	for _, e := range r.claims {
		if isPrefixPath(path, e.Path) && e.Path > after {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]Entry, 0, len(r.claims))
	for _, e := range r.claims {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return Snapshot{Generation: r.generation, NextRevision: r.nextRevision, Entries: entries}
}
