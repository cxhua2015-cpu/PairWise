package readyqueue300

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	c := &Queue{
		opts:         q.opts,
		items:        make(map[string]Item, len(q.items)),
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
	}
	for id, it := range q.items {
		c.items[id] = it
	}
	return c, nil
}
