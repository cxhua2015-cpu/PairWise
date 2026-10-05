package rankboard

import (
	"errors"
	"math"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrScore          = errors.New("invalid score")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Add Kind = iota + 1
	Set
	Delete
)

type Options struct {
	MaxItems, MaxIDBytes int
	MaxAbsScore          int64
}
type Op struct {
	Kind         Kind
	ID           string
	Delta, Score int64
}
type Batch struct{ Ops []Op }
type Item struct {
	ID       string
	Score    int64
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Item
}
type Snapshot struct {
	Generation, NextRevision uint64
	Items                    []Item
}

type Board struct {
	mu         sync.Mutex
	maxItems   int
	maxIDBytes int
	maxAbs     int64
	items      map[string]Item
	generation uint64
	revision   uint64
}

func New(o Options) (*Board, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 || o.MaxAbsScore <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Board{
		maxItems:   o.MaxItems,
		maxIDBytes: o.MaxIDBytes,
		maxAbs:     o.MaxAbsScore,
		items:      make(map[string]Item),
	}, nil
}

func (b *Board) validID(id string) bool {
	if id == "" || len(id) > b.maxIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return false
		}
	}
	return true
}

func (b *Board) validate(op Op) error {
	if !b.validID(op.ID) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Add:
		if op.Delta == 0 || op.Score != 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Delta != 0 || op.Score > b.maxAbs || op.Score < -b.maxAbs {
			return ErrInvalidInput
		}
	case Delete:
		if op.Delta != 0 || op.Score != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (b *Board) Apply(batch Batch) (Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, op := range batch.Ops {
		if err := b.validate(op); err != nil {
			return Result{}, err
		}
	}
	if len(batch.Ops) == 0 {
		return Result{}, nil
	}

	candidate := make(map[string]Item, len(b.items)+len(batch.Ops))
	for id, it := range b.items {
		candidate[id] = it
	}
	revision := b.revision
	touched := make(map[string]struct{}, len(batch.Ops))

	for _, op := range batch.Ops {
		switch op.Kind {
		case Add:
			it := candidate[op.ID]
			it.ID = op.ID
			if op.Delta > 0 && it.Score > math.MaxInt64-op.Delta {
				return Result{}, ErrScore
			}
			if op.Delta < 0 && it.Score < math.MinInt64-op.Delta {
				return Result{}, ErrScore
			}
			it.Score += op.Delta
			if it.Score > b.maxAbs || it.Score < -b.maxAbs {
				return Result{}, ErrScore
			}
			revision++
			it.Revision = revision
			candidate[op.ID] = it
			touched[op.ID] = struct{}{}
		case Set:
			revision++
			candidate[op.ID] = Item{ID: op.ID, Score: op.Score, Revision: revision}
			touched[op.ID] = struct{}{}
		case Delete:
			if _, ok := candidate[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.ID)
		}
	}

	if len(candidate) > b.maxItems {
		return Result{}, ErrCapacity
	}

	b.items = candidate
	b.revision = revision
	b.generation++

	changed := make([]Item, 0, len(touched))
	for id := range touched {
		if it, ok := candidate[id]; ok {
			changed = append(changed, it)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].ID < changed[j].ID })

	return Result{Generation: b.generation, Revision: revision, Changed: changed}, nil
}

func (b *Board) Top(limit int) ([]Item, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	items := make([]Item, 0, len(b.items))
	for _, it := range b.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].ID < items[j].ID
	})
	if limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

func (b *Board) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	items := make([]Item, 0, len(b.items))
	for _, it := range b.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return Snapshot{Generation: b.generation, NextRevision: b.revision + 1, Items: items}
}
