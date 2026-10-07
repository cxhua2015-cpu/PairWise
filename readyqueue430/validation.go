package readyqueue430

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. It shares its structural semantics with Apply.
func (q *Queue) ValidateBatch(b Batch) error {
	return q.validateBatch(b)
}

// validateBatch checks only the shape of the batch: non-negative time,
// known kinds, well-formed IDs within the configured byte limit, and no
// extra fields on Cancel ops. It never touches queue state.
func (q *Queue) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if !q.validID(op.ID) || op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			if !q.validID(op.ID) || op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validID reports whether id is non-empty, within the byte limit, and
// contains only ASCII lowercase letters, digits, hyphens and underscores.
func (q *Queue) validID(id string) bool {
	if id == "" || len(id) > q.maxIDBytes {
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
