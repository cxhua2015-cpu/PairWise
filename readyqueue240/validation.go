package readyqueue240

// validateBatch performs complete structural validation of a batch
// without reading or mutating any queue state. Apply and ValidateBatch
// share this exact structural semantics.
func validateBatch(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateOp(op, maxIDBytes); err != nil {
			return err
		}
	}
	return nil
}

func validateOp(op Op, maxIDBytes int) error {
	switch op.Kind {
	case Enqueue:
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	case Cancel:
		// Cancel carries no payload; extra fields are rejected.
		if op.Priority != 0 || op.ReadyAt != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validateID(op.ID, maxIDBytes)
}

// validateID allows only non-empty ASCII lowercase letters, digits,
// hyphens and underscores, bounded by the configured byte limit.
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
	return validateBatch(b, q.maxIDBytes)
}
