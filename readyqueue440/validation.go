package readyqueue440

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. It shares structural semantics with Apply.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if !validID(op.ID, q.maxIDBytes) || op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			// Cancel carries no extra fields.
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
			if !validID(op.ID, q.maxIDBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

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
