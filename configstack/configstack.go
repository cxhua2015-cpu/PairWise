package configstack

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	AddLayer Kind = iota + 1
	RemoveLayer
	Set
	Delete
	Move
)

type Options struct{ MaxLayers, MaxEntries, MaxNameBytes, MaxKeyBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind      Kind
	Name, Key string
	Value     []byte
	Position  int
}
type Batch struct{ Ops []Op }
type Entry struct {
	Layer, Key string
	Value      []byte
	Revision   uint64
}
type Layer struct {
	Name    string
	Entries []Entry
}
type Result struct {
	Generation, Revision uint64
	Changed              []Entry
}
type Snapshot struct {
	Generation, NextRevision uint64
	Layers                   []Layer
}

type entryState struct {
	value    []byte
	revision uint64
}

type layerState struct {
	name    string
	entries map[string]*entryState
}

type Stack struct {
	mu     sync.RWMutex
	opts   Options
	layers []*layerState
	gen    uint64
	rev    uint64
}

func New(o Options) (*Stack, error) {
	if o.MaxLayers <= 0 || o.MaxEntries <= 0 || o.MaxNameBytes <= 0 ||
		o.MaxKeyBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Stack{opts: o}, nil
}

func validToken(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func (s *Stack) validate(op Op) error {
	switch op.Kind {
	case AddLayer:
		if !validToken(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil ||
			op.Position < 0 || op.Position > s.opts.MaxLayers-1 {
			return ErrInvalidInput
		}
	case RemoveLayer:
		if !validToken(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil || op.Position != 0 {
			return ErrInvalidInput
		}
	case Set:
		if !validToken(op.Name, s.opts.MaxNameBytes) || !validToken(op.Key, s.opts.MaxKeyBytes) ||
			op.Value == nil || len(op.Value) > s.opts.MaxValueBytes || op.Position != 0 {
			return ErrInvalidInput
		}
	case Delete:
		if !validToken(op.Name, s.opts.MaxNameBytes) || !validToken(op.Key, s.opts.MaxKeyBytes) ||
			op.Value != nil || op.Position != 0 {
			return ErrInvalidInput
		}
	case Move:
		if !validToken(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil || op.Position < 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func cloneLayers(in []*layerState) []*layerState {
	out := make([]*layerState, len(in))
	for i, l := range in {
		nl := &layerState{name: l.name, entries: make(map[string]*entryState, len(l.entries))}
		for k, e := range l.entries {
			nl.entries[k] = &entryState{value: append([]byte(nil), e.value...), revision: e.revision}
		}
		out[i] = nl
	}
	return out
}

func findLayer(layers []*layerState, name string) int {
	for i, l := range layers {
		if l.name == name {
			return i
		}
	}
	return -1
}

func (s *Stack) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, op := range b.Ops {
		if err := s.validate(op); err != nil {
			return Result{}, err
		}
	}

	cand := cloneLayers(s.layers)
	rev := s.rev
	type change struct {
		layer, key string
		revision   uint64
	}
	changed := map[string]change{}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddLayer:
			if findLayer(cand, op.Name) >= 0 {
				return Result{}, ErrExists
			}
			if op.Position > len(cand) {
				return Result{}, ErrInvalidInput
			}
			nl := &layerState{name: op.Name, entries: map[string]*entryState{}}
			cand = append(cand, nil)
			copy(cand[op.Position+1:], cand[op.Position:])
			cand[op.Position] = nl
		case RemoveLayer:
			i := findLayer(cand, op.Name)
			if i < 0 {
				return Result{}, ErrNotFound
			}
			cand = append(cand[:i], cand[i+1:]...)
		case Set:
			i := findLayer(cand, op.Name)
			if i < 0 {
				return Result{}, ErrNotFound
			}
			rev++
			cand[i].entries[op.Key] = &entryState{
				value:    append([]byte(nil), op.Value...),
				revision: rev,
			}
			changed[op.Name+"\x00"+op.Key] = change{layer: op.Name, key: op.Key, revision: rev}
		case Delete:
			i := findLayer(cand, op.Name)
			if i < 0 {
				return Result{}, ErrNotFound
			}
			if _, ok := cand[i].entries[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand[i].entries, op.Key)
		case Move:
			i := findLayer(cand, op.Name)
			if i < 0 {
				return Result{}, ErrNotFound
			}
			l := cand[i]
			rest := append(cand[:i:i], cand[i+1:]...)
			if op.Position > len(rest) {
				return Result{}, ErrInvalidInput
			}
			cand = append(rest, nil)
			copy(cand[op.Position+1:], cand[op.Position:])
			cand[op.Position] = l
		}
	}

	entries, bytes := 0, 0
	for _, l := range cand {
		entries += len(l.entries)
		for _, e := range l.entries {
			bytes += len(e.value)
		}
	}
	if len(cand) > s.opts.MaxLayers || entries > s.opts.MaxEntries || bytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		s.gen++
	}
	s.layers = cand
	s.rev = rev

	res := Result{Generation: s.gen, Revision: s.rev}
	for _, l := range cand {
		keys := make([]string, 0, len(l.entries))
		for k := range l.entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c, ok := changed[l.name+"\x00"+k]
			if !ok {
				continue
			}
			e := l.entries[k]
			res.Changed = append(res.Changed, Entry{
				Layer:    l.name,
				Key:      k,
				Value:    append([]byte(nil), e.value...),
				Revision: c.revision,
			})
		}
	}
	return res, nil
}

func (s *Stack) Resolve(key string) (Entry, bool, error) {
	if !validToken(key, s.opts.MaxKeyBytes) {
		return Entry{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.layers {
		if e, ok := l.entries[key]; ok {
			return Entry{
				Layer:    l.name,
				Key:      key,
				Value:    append([]byte(nil), e.value...),
				Revision: e.revision,
			}, true, nil
		}
	}
	return Entry{}, false, nil
}

func (s *Stack) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.gen, NextRevision: s.rev + 1}
	for _, l := range s.layers {
		keys := make([]string, 0, len(l.entries))
		for k := range l.entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		nl := Layer{Name: l.name, Entries: make([]Entry, 0, len(keys))}
		for _, k := range keys {
			e := l.entries[k]
			nl.Entries = append(nl.Entries, Entry{
				Layer:    l.name,
				Key:      k,
				Value:    append([]byte(nil), e.value...),
				Revision: e.revision,
			})
		}
		snap.Layers = append(snap.Layers, nl)
	}
	return snap
}
