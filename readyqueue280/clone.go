package readyqueue280

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the original: it has its own mutex and
// item map, so subsequent mutations of either queue never alias the other.
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
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
	}, nil
}
