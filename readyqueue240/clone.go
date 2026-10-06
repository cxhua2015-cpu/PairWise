package readyqueue240

import "maps"

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutations on either
// queue never alias the other's state.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return &Queue{
		maxItems:     q.maxItems,
		maxIDBytes:   q.maxIDBytes,
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
		items:        maps.Clone(q.items),
	}, nil
}
