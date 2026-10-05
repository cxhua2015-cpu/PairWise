package expirytable424

// Preview simulates Apply from one linearizable snapshot without mutating the receiver.
func (x *Table) Preview(Batch) (Result, Snapshot, Stats, error) {
	return Result{}, Snapshot{}, Stats{}, ErrNotImplemented
}
