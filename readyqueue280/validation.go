package readyqueue280

// validateBatchStructural performs complete structural validation of a
// batch without reading or mutating any queue state. Apply and
// ValidateBatch share this exact structural semantics; maxIDBytes is the
// configured Options byte cap on IDs.
func validateBatchStructural(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validID(op.ID) || len(op.ID) > maxIDBytes {
			return ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
		if op.Kind == Cancel && (op.Priority != 0 || op.ReadyAt != 0) {
			return ErrInvalidInput
		}
	}
	return nil
}

// validID reports whether id is a non-empty ASCII identifier of lowercase
// letters, digits, hyphens and underscores.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating state. Options are immutable after New, so no lock is needed.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStructural(b, q.opts.MaxIDBytes)
}
