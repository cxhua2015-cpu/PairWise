package readyqueue270

// validateBatchStruct performs complete structural validation of a batch.
// It is side-effect free and never reads queue state, so Apply and
// ValidateBatch share exactly the same structural semantics.
func validateBatchStruct(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateID(op.ID, maxIDBytes); err != nil {
			return err
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
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateID enforces the name/key rules: non-empty, at most maxIDBytes
// bytes, ASCII lowercase letters, digits, hyphens and underscores only.
func validateID(id string, maxIDBytes int) error {
	if len(id) == 0 || len(id) > maxIDBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStruct(b, q.maxIDBytes)
}
