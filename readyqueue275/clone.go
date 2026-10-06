package readyqueue275

// Clone returns a fully independent deep copy, including the logical
// clocks (generation, nextRevision, now). The clone shares no memory
// with the original: subsequent Apply/Pop on either queue can never
// alias the other's state.
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
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
		items:        items,
	}, nil
}
