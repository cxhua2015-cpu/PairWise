package readyqueue275

// Clone returns a fully independent deep copy, including logical clocks.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make(map[string]Item, len(q.items))
	for k, v := range q.items {
		items[k] = v
	}
	return &Queue{
		opts:    q.opts,
		items:   items,
		now:     q.now,
		gen:     q.gen,
		nextRev: q.nextRev,
	}, nil
}
