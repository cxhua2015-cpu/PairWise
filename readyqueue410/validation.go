package readyqueue410

// validID reports whether id is a non-empty ASCII lowercase/digit/hyphen/
// underscore string within the configured byte limit.
func validID(id string, maxIDBytes int) bool {
	if len(id) == 0 || len(id) > maxIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// validateBatch performs complete structural validation of a batch without
// reading or mutating queue state. Apply and ValidateBatch share it so both
// enforce identical structural semantics.
func validateBatch(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validID(op.ID, maxIDBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Enqueue:
			if op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatch(b, q.maxIDBytes)
}
