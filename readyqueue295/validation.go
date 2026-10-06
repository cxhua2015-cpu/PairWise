package readyqueue295

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. Apply shares this exact structural preflight so a
// batch rejected here can never partially apply.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatch(b, q.maxIDBytes)
}

func validateBatch(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validID(op.ID, maxIDBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Enqueue:
			if op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			// Cancel carries no payload; any extra field is invalid.
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validID reports whether id is a non-empty ASCII lowercase alnum, hyphen, or
// underscore string within the configured byte limit.
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
