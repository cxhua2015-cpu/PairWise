package expirytable439

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (x *Table) Preview(b Batch) (Result, Snapshot, Stats, error) {
	c, err := x.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	r, err := c.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return r, c.Snapshot(), c.Stats(), nil
}
