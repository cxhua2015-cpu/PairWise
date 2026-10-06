package readyqueue270

import "container/heap"

// Clone returns a fully independent deep copy, including the logical clocks
// (now, generation, next revision). The clone shares no memory with the
// original: entries, indexes and the mutex are all freshly allocated, so
// later transactions on either queue never affect the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := &Queue{
		opts:    q.opts,
		now:     q.now,
		gen:     q.gen,
		nextRev: q.nextRev,
		byID:    make(map[string]*entry, len(q.byID)),
		ready:   make(readyHeap, 0, len(q.byID)),
	}
	for _, e := range q.byID {
		ne := &entry{item: e.item, heap: -1}
		c.byID[ne.item.ID] = ne
		c.ready = append(c.ready, ne)
	}
	for i, e := range c.ready {
		e.heap = i
	}
	heap.Init(&c.ready)
	return c, nil
}
