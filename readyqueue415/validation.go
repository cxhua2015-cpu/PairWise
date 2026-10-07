package readyqueue415

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error { return q.validateBatch(b) }

// validateBatch is the shared structural preflight used by both
// ValidateBatch and Apply. It never touches queue state.
func (q *Queue) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validID(op.ID, q.opts.MaxIDBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Cancel && (op.Priority != 0 || op.ReadyAt != 0) {
			return ErrInvalidInput
		}
		if op.Kind == Enqueue && op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

// validID reports whether id is a non-empty ASCII identifier of at most
// maxBytes bytes, using only lowercase letters, digits, '-' and '_'.
func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
