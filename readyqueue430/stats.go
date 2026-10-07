package readyqueue430

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state: the lock
// is taken once, so all four fields describe a single instant.
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
