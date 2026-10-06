package readyqueue295

// validID reports whether id is a non-empty ASCII lowercase
// alphanumeric/hyphen/underscore key within the configured byte limit.
func validID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
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

// checkBatch is the single source of structural batch semantics shared by
// Apply and ValidateBatch. It reads only immutable configuration.
func (q *Queue) checkBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validID(op.ID, q.maxIDBytes) {
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
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return q.checkBatch(b)
}
