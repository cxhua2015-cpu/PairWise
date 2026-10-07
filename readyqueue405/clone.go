package readyqueue405

// Clone returns a fully independent deep copy, including logical clocks
// (now, generation, nextRevision). The clone shares no ownership with the
// original: subsequent transactions on either queue never affect the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := &Queue{
		opts:         q.opts,
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
