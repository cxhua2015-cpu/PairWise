package readyqueue250

// Clone returns a fully independent deep copy, including logical clocks.
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
		items:        items,
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
	}, nil
}
