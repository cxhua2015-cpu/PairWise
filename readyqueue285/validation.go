package readyqueue285

// validID reports whether id is a non-empty ASCII [a-z0-9-_] string
// within the configured byte limit.
func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
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

// validateOp checks one op structurally, without touching queue state.
func validateOp(op Op, maxIDBytes int) error {
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
	if !validID(op.ID, maxIDBytes) {
		return ErrInvalidInput
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. Apply shares these exact structural semantics.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateOp(op, q.maxIDBytes); err != nil {
			return err
		}
	}
	return nil
}
