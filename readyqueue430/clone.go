package readyqueue430

// Clone returns a fully independent deep copy, including logical clocks
// (now, generation, nextRevision). The clone owns a fresh map and a
// fresh mutex, so no subsequent operation on either queue can alias or
// block the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
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
