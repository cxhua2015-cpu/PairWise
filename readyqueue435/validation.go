package readyqueue435

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

// validateBatch is the shared structural validation used by Apply and the
// public ValidateBatch precheck. It never reads or mutates queue state.
func (q *Queue) validateBatch(b Batch) error {
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
			if !validID(op.ID, q.maxIDBytes) || op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error { return q.validateBatch(b) }
