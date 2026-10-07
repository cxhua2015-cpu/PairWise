package readyqueue440

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.cloneLocked(), nil
}

func (q *Queue) cloneLocked() *Queue {
	items := make(map[string]Item, len(q.items))
	for id, it := range q.items {
		items[id] = it
	}
	return &Queue{
		maxItems:     q.maxItems,
		maxIDBytes:   q.maxIDBytes,
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
		items:        items,
	}
}
