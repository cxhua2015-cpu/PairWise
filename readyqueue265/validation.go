package readyqueue265

// ValidateBatch performs complete structural validation without reading or mutating state.
// Apply shares this exact structural pre-check before touching any state.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateID(op.ID, q.maxIDBytes); err != nil {
			return err
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
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validateID(id string, maxBytes int) error {
	if len(id) == 0 || len(id) > maxBytes {
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
