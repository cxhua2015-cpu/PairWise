package readyqueue440

// validID reports whether id is a non-empty ASCII identifier of at most
// maxBytes bytes, using only lowercase letters, digits, '-' and '_'.
func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. It shares its structural semantics with Apply:
// negative batch time, unknown kinds, invalid IDs, negative ReadyAt, and
// non-zero Priority/ReadyAt on Cancel ops are all rejected.
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
			if !validID(op.ID, q.maxIDBytes) || op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}
