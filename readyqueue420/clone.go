package readyqueue420

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for k, v := range q.items {
		items[k] = v
	}
	return &Queue{
		opts:         q.opts,
		items:        items,
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
	}, nil
}
