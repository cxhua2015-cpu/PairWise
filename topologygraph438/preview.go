package topologygraph438

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(b Batch) (Result, Snapshot, Stats, error) {
	c, err := g.Clone()
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	res, err := c.Apply(b)
	if err != nil {
		return Result{}, Snapshot{}, Stats{}, err
	}
	return res, c.Snapshot(), c.Stats(), nil
}
