package readyqueue435

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStructural(b, q.maxIDBytes)
}
