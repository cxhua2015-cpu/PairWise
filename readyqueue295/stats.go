package readyqueue295

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state: the values are
// read under the same mutex that serializes Apply and Pop, so they always
// reflect a single point in the queue's history.
func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Stats{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        len(q.items),
	}
}
