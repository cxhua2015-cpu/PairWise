package expirytable424

// Preview simulates Apply from one linearizable snapshot without mutating the
// receiver. The returned Result, candidate Snapshot and candidate Stats are
// exactly what committing the batch on that snapshot would produce, and all
// slices are independently owned. On failure the error matches Apply on the
// same state and every returned value is zero.
func (t *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}

	t.mu.Lock()
	if b.Now < t.st.now {
		t.mu.Unlock()
		return Result{}, Snapshot{}, Stats{}, ErrTime
	}
	cand := t.st.cloneState()
	t.mu.Unlock()

	next, res, err := cand.commitCandidate(t.opt, b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, next.snapshot(), next.stats(), nil
}
