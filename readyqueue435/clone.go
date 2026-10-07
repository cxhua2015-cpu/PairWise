package readyqueue435

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either queue
// never affects the other.
func (q *Queue) Clone() (*Queue, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return &Queue{
		maxItems:     q.maxItems,
		maxIDBytes:   q.maxIDBytes,
		items:        cloneItems(q.items),
		generation:   q.generation,
		nextRevision: q.nextRevision,
		now:          q.now,
	}, nil
}
