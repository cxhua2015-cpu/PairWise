package expirytable429

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (t *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	c, err := t.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	r, err := c.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return r, c.Snapshot(), c.Stats(), nil
}
