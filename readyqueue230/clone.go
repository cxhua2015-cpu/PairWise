package readyqueue230

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the source: entries are re-allocated and
// the heap is rebuilt, so later mutations of either queue never alias.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := &Queue{
		opts:         q.opts,
		items:        make(map[string]*entry, len(q.items)),
		ready:        make(entryHeap, 0, len(q.ready)),
		now:          q.now,
		generation:   q.generation,
		nextRevision: q.nextRevision,
	}
	for _, e := range q.ready {
		ne := &entry{item: e.item}
		c.items[ne.item.ID] = ne
		c.ready = append(c.ready, ne)
	}
	return c, nil
}
