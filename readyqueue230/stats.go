package readyqueue230

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state: it takes the
// same mutex as the transaction engine, so the reported generation,
// revision counter, clock and item count are mutually consistent.
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
