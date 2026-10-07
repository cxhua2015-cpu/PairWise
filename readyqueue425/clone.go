package readyqueue425

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original: subsequent
// mutations of either queue never affect the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for id, it := range q.items {
		items[id] = it
	}
	return &Queue{
		opts:         q.opts,
		items:        items,
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
	}, nil
}
