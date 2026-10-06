package readyqueue280

// validateStructural performs complete structural validation of a batch
// without reading or mutating any queue state. It is the single source of
// structural semantics shared by Apply and ValidateBatch.
func validateStructural(b Batch, maxIDBytes int) error {
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
			// Cancel carries no payload; extra fields are rejected.
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func validID(id string, maxIDBytes int) bool {
	if len(id) == 0 || len(id) > maxIDBytes {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateStructural(b, q.maxIDBytes)
}
