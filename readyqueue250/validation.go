package readyqueue250

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if op.Kind == Cancel && (op.Priority != 0 || op.ReadyAt != 0) {
			return ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDBytes) {
			return ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

// validID reports whether id is non-empty, within the byte limit, and
// contains only ASCII lowercase letters, digits, hyphens and underscores.
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
