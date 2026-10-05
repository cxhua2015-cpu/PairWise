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

type layerState struct {
	name    string
	entries map[string]entryState
}

type entryState struct {
	value    []byte
	revision uint64
}

type Stack struct {
	mu         sync.RWMutex
	opts       Options
	layers     []*layerState
	byName     map[string]*layerState
	generation uint64
	revision   uint64
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func New(o Options) (*Stack, error) {
	if o.MaxLayers <= 0 || o.MaxEntries <= 0 || o.MaxNameBytes <= 0 ||
		o.MaxKeyBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Stack{opts: o, byName: make(map[string]*layerState)}, nil
}

// validateOp checks a single op structurally, without reading stack state.
func (s *Stack) validateOp(op Op) error {
	switch op.Kind {
	case AddLayer:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil {
			return ErrInvalidInput
		}
		if op.Position < 0 || op.Position > s.opts.MaxLayers-1 {
			return ErrInvalidInput
		}
	case RemoveLayer:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil || op.Position != 0 {
			return ErrInvalidInput
		}
	case Set:
		if !validName(op.Name, s.opts.MaxNameBytes) || !validName(op.Key, s.opts.MaxKeyBytes) || op.Position != 0 {
			return ErrInvalidInput
		}
		if op.Value == nil || len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) || !validName(op.Key, s.opts.MaxKeyBytes) ||
			op.Value != nil || op.Position != 0 {
			return ErrInvalidInput
		}
	case Move:
		if !validName(op.Name, s.opts.MaxNameBytes) || op.Key != "" || op.Value != nil || op.Position < 0 {
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
		nl := &layerState{name: l.name, entries: make(map[string]entryState, len(l.entries))}
		for k, e := range l.entries {
			nl.entries[k] = e
		}
		out[i] = nl
	}
	return out
}

func (s *Stack) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	layers := cloneLayers(s.layers)
	byName := make(map[string]*layerState, len(layers))
	for _, l := range layers {
		byName[l.name] = l
	}
	revision := s.revision
	touched := make(map[*layerState]map[string]bool)

	find := func(name string) (int, *layerState) {
		l := byName[name]
		if l == nil {
			return -1, nil
		}
		for i, x := range layers {
			if x == l {
				return i, l
			}
		}
		return -1, nil
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case AddLayer:
			if byName[op.Name] != nil {
				return Result{}, ErrExists
			}
			if op.Position > len(layers) {
				return Result{}, ErrInvalidInput
			}
			nl := &layerState{name: op.Name, entries: make(map[string]entryState)}
			layers = append(layers, nil)
			copy(layers[op.Position+1:], layers[op.Position:])
			layers[op.Position] = nl
			byName[op.Name] = nl
		case RemoveLayer:
			i, l := find(op.Name)
			if l == nil {
				return Result{}, ErrNotFound
			}
			layers = append(layers[:i], layers[i+1:]...)
			delete(byName, op.Name)
			delete(touched, l)
		case Set:
			_, l := find(op.Name)
			if l == nil {
				return Result{}, ErrNotFound
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			l.entries[op.Key] = entryState{value: v, revision: revision}
			if touched[l] == nil {
				touched[l] = make(map[string]bool)
			}
			touched[l][op.Key] = true
		case Delete:
			_, l := find(op.Name)
			if l == nil {
				return Result{}, ErrNotFound
			}
			if _, ok := l.entries[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(l.entries, op.Key)
			if touched[l] != nil {
				delete(touched[l], op.Key)
			}
		case Move:
			i, l := find(op.Name)
			if l == nil {
				return Result{}, ErrNotFound
			}
			rest := append(layers[:i], layers[i+1:]...)
			if op.Position > len(rest) {
				return Result{}, ErrInvalidInput
			}
			rest = append(rest, nil)
			copy(rest[op.Position+1:], rest[op.Position:])
			rest[op.Position] = l
			layers = rest
		}
	}

	entries, bytes := 0, 0
	for _, l := range layers {
		entries += len(l.entries)
		for _, e := range l.entries {
			bytes += len(e.value)
		}
	}
	if len(layers) > s.opts.MaxLayers || entries > s.opts.MaxEntries || bytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	res := Result{Generation: s.generation, Revision: revision}
	if len(b.Ops) > 0 {
		s.generation++
		res.Generation = s.generation
	}
	s.layers = layers
	s.byName = byName
	s.revision = revision

	for _, l := range layers {
		keys := touched[l]
		if len(keys) == 0 {
			continue
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			if _, ok := l.entries[k]; ok {
				sorted = append(sorted, k)
			}
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			e := l.entries[k]
			v := make([]byte, len(e.value))
			copy(v, e.value)
			res.Changed = append(res.Changed, Entry{Layer: l.name, Key: k, Value: v, Revision: e.revision})
		}
	}
	return res, nil
}

func (s *Stack) Resolve(key string) (Entry, bool, error) {
	if !validName(key, s.opts.MaxKeyBytes) {
		return Entry{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.layers {
		if e, ok := l.entries[key]; ok {
			v := make([]byte, len(e.value))
			copy(v, e.value)
			return Entry{Layer: l.name, Key: key, Value: v, Revision: e.revision}, true, nil
		}
	}
	return Entry{}, false, nil
}

func (s *Stack) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	for _, l := range s.layers {
		nl := Layer{Name: l.name}
		keys := make([]string, 0, len(l.entries))
		for k := range l.entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e := l.entries[k]
			v := make([]byte, len(e.value))
			copy(v, e.value)
			nl.Entries = append(nl.Entries, Entry{Layer: l.name, Key: k, Value: v, Revision: e.revision})
		}
		snap.Layers = append(snap.Layers, nl)
	}
	return snap
}
