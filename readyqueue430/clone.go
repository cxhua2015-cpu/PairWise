package readyqueue430

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either queue
// never affects the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.cloneLocked(), nil
}

// cloneLocked deep-copies the queue; the caller must hold q.mu.
func (q *Queue) cloneLocked() *Queue {
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
	}
}
