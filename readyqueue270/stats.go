package readyqueue270

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state: it is taken
// under the same lock that serializes Apply and Pop, so it always reflects a
// position between two committed transactions, never a partial one.
func (q *Queue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Stats{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        len(q.byID),
	}
}
