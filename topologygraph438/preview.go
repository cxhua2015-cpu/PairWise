package topologygraph438

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (g *Graph) Preview(Batch) (Result, Snapshot, Stats, error) {
	return Result{}, Snapshot{}, Stats{}, ErrNotImplemented
}
