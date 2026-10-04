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
	mu         sync.Mutex
	maxClaims  int
	maxPath    int
	maxOwner   int
	claims     map[string]Entry
	generation uint64
	revision   uint64
}

func New(o Options) (*Registry, error) {
	if o.MaxClaims <= 0 || o.MaxPathBytes <= 0 || o.MaxOwnerBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Registry{
		maxClaims: o.MaxClaims,
		maxPath:   o.MaxPathBytes,
		maxOwner:  o.MaxOwnerBytes,
		claims:    map[string]Entry{},
	}, nil
}

func validPathChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '-'
}

func validOwnerChar(c byte) bool {
	return validPathChar(c) || c == '/'
}

func (r *Registry) validPath(p string) bool {
	if len(p) == 0 || len(p) > r.maxPath || p[0] != '/' {
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
			if !validPathChar(seg[i]) {
				return false
			}
		}
	}
	return true
}

func (r *Registry) validOwner(o string) bool {
	if len(o) == 0 || len(o) > r.maxOwner {
		return false
	}
	for i := 0; i < len(o); i++ {
		if !validOwnerChar(o[i]) {
			return false
		}
	}
	return true
}

// isAncestor reports whether a is a proper ancestor of b (segment-aware).
func isAncestor(a, b string) bool {
	if a == "/" {
		return b != "/"
	}
	return strings.HasPrefix(b, a+"/")
}

func pathsConflict(a, b string) bool {
	return a == b || isAncestor(a, b) || isAncestor(b, a)
}

func (r *Registry) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if op.Kind != Claim && op.Kind != Release {
			return Result{}, ErrInvalidInput
		}
		if !r.validPath(op.Path) || !r.validOwner(op.Owner) {
			return Result{}, ErrInvalidInput
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	candidate := make(map[string]Entry, len(r.claims))
	for k, v := range r.claims {
		candidate[k] = v
	}
	revision := r.revision
	touched := map[string]struct{}{}

	for _, op := range b.Ops {
		switch op.Kind {
		case Claim:
			for p := range candidate {
				if pathsConflict(p, op.Path) {
					return Result{}, ErrConflict
				}
			}
			revision++
			candidate[op.Path] = Entry{Path: op.Path, Owner: op.Owner, Revision: revision}
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
			touched[op.Path] = struct{}{}
		}
	}

	if len(candidate) > r.maxClaims {
		return Result{}, ErrCapacity
	}

	r.claims = candidate
	r.revision = revision
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

	return Result{Generation: r.generation, Revision: r.revision, Changed: changed}, nil
}

func (r *Registry) Lookup(path string) (Entry, bool, error) {
	if !r.validPath(path) {
		return Entry{}, false, ErrInvalidInput
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		if e, ok := r.claims[path]; ok {
			return e, true, nil
		}
		if path == "/" {
			return Entry{}, false, nil
		}
		i := strings.LastIndexByte(path, '/')
		if i == 0 {
			path = "/"
		} else {
			path = path[:i]
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
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, 0, limit)
	for _, e := range r.claims {
		if isAncestor(path, e.Path) && e.Path > after {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]Entry, 0, len(r.claims))
	for _, e := range r.claims {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return Snapshot{Generation: r.generation, NextRevision: r.revision + 1, Entries: entries}
}
