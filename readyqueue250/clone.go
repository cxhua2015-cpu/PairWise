package readyqueue250

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	c := &Queue{
		maxItems:     q.maxItems,
		maxIDBytes:   q.maxIDBytes,
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
		items:        make(map[string]Item, len(q.items)),
	}
	for id, it := range q.items {
		c.items[id] = it
	}
	return c, nil
}
