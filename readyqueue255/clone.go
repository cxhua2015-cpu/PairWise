package readyqueue255

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either queue
// never affects the other.
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
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
	}, nil
}
