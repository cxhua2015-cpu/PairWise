package readyqueue415

// validID reports whether id is a non-empty ASCII identifier of at most
// maxIDBytes bytes, using only [a-z0-9-_].
func validID(id string, maxIDBytes int) bool {
	if len(id) == 0 || len(id) > maxIDBytes {
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

// validateOpStructure checks a single op without touching queue state.
func validateOpStructure(op Op, maxIDBytes int) error {
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
	default:
		return ErrInvalidInput
	}
	return nil
}

// validateBatchStructure is the shared structural contract used by both
// ValidateBatch and Apply: non-negative batch time and well-formed ops.
func validateBatchStructure(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateOpStructure(op, maxIDBytes); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStructure(b, q.maxIDBytes)
}
