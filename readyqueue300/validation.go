package readyqueue300

// validID reports whether id is a non-empty ASCII lowercase/digit/'-'/'_'
// string within the configured byte limit.
func validID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateStructural performs complete structural validation of a batch
// without reading or mutating queue state. Unknown kinds and fields that
// are not meaningful for a kind (non-zero Priority/ReadyAt on Cancel)
// are rejected with ErrInvalidInput.
func (q *Queue) validateStructural(b Batch) error {
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
	return q.validateStructural(b)
}
