package readyqueue235

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for id, it := range q.items {
		items[id] = it
	}
	return &Queue{
		opts:    q.opts,
		now:     q.now,
		gen:     q.gen,
		nextRev: q.nextRev,
		items:   items,
	}, nil
}
