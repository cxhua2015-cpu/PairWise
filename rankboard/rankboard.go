package rankboard

import (
	"errors"
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
	mu         sync.RWMutex
	opts       Options
	items      map[string]Item
	generation uint64
	revision   uint64
}

func New(o Options) (*Board, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 || o.MaxAbsScore <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Board{opts: o, items: make(map[string]Item)}, nil
}

func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
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
	if !validID(op.ID, b.opts.MaxIDBytes) {
		return ErrInvalidInput
	}
	switch op.Kind {
	case Add:
		if op.Delta == 0 || op.Score != 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Delta != 0 || op.Score > b.opts.MaxAbsScore || op.Score < -b.opts.MaxAbsScore {
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
	for _, op := range batch.Ops {
		if err := b.validate(op); err != nil {
			return Result{}, err
		}
	}
	if len(batch.Ops) == 0 {
		return Result{}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	candidate := make(map[string]Item, len(b.items)+len(batch.Ops))
	for id, it := range b.items {
		candidate[id] = it
	}
	touched := make(map[string]Item, len(batch.Ops))
	revision := b.revision

	for _, op := range batch.Ops {
		switch op.Kind {
		case Add:
			it := candidate[op.ID]
			score := it.Score + op.Delta
			if (op.Delta > 0 && score < it.Score) || (op.Delta < 0 && score > it.Score) {
				return Result{}, ErrScore
			}
			if score > b.opts.MaxAbsScore || score < -b.opts.MaxAbsScore {
				return Result{}, ErrScore
			}
			revision++
			it.ID = op.ID
			it.Score = score
			it.Revision = revision
			candidate[op.ID] = it
			touched[op.ID] = it
		case Set:
			revision++
			it := Item{ID: op.ID, Score: op.Score, Revision: revision}
			candidate[op.ID] = it
			touched[op.ID] = it
		case Delete:
			if _, ok := candidate[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.ID)
			delete(touched, op.ID)
		}
	}

	if len(candidate) > b.opts.MaxItems {
		return Result{}, ErrCapacity
	}

	b.items = candidate
	b.revision = revision
	b.generation++

	changed := make([]Item, 0, len(touched))
	for _, it := range touched {
		changed = append(changed, it)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].ID < changed[j].ID })

	return Result{Generation: b.generation, Revision: b.revision, Changed: changed}, nil
}

func (b *Board) Top(limit int) ([]Item, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrInvalidInput
	}
	b.mu.RLock()
	items := make([]Item, 0, len(b.items))
	for _, it := range b.items {
		items = append(items, it)
	}
	b.mu.RUnlock()
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
	b.mu.RLock()
	defer b.mu.RUnlock()
	var items []Item
	if len(b.items) > 0 {
		items = make([]Item, 0, len(b.items))
		for _, it := range b.items {
			items = append(items, it)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	}
	return Snapshot{Generation: b.generation, NextRevision: b.revision + 1, Items: items}
}
