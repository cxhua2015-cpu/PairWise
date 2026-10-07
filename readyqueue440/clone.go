package readyqueue440

// Clone returns a fully independent deep copy of the queue, preserving the
// logical clock (now), generation and next revision. The clone shares no
// mutable state with the original, so subsequent operations on either
// queue never alias the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for id, it := range q.items {
		items[id] = it
	}
	return &Queue{
		maxItems:     q.maxItems,
		maxIDBytes:   q.maxIDBytes,
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
		items:        items,
	}, nil
}
