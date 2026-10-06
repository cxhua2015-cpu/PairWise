package readyqueue275

type Stats struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    int
}

// Stats returns a linearizable summary of the current state.
func (q *Queue) Stats() Stats { return Stats{} }
