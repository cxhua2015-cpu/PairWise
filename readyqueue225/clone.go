package readyqueue225

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for k, v := range q.items {
		items[k] = v
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
