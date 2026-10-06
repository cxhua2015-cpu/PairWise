package readyqueue275

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state: it is
// taken under the same mutex that serializes Apply/Pop, so it always
// reflects a single consistent point in the transaction history.
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
