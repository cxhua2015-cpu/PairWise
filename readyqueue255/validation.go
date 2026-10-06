package readyqueue255

// validateID reports whether id is a non-empty ASCII [a-z0-9-_] string
// within the configured byte limit.
func validateID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
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

// validateBatchStruct performs complete structural validation of a batch.
// It reads no queue state and has no side effects; Apply shares it so both
// paths agree on structural semantics.
func validateBatchStruct(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validateID(op.ID, maxIDBytes) {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStruct(b, q.maxIDBytes)
}
