package expirytable419

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply before any time check.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !t.validateOp(b.Now, op) {
			return ErrInvalidInput
		}
	}
	return nil
}
