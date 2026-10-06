package readyqueue290

// validateBatch is the shared structural semantics used by both
// ValidateBatch and Apply. It is side-effect free and reads no state.
func (q *Queue) validateBatch(b Batch) error {
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
		if !q.validID(op.ID) {
			return ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

func (q *Queue) validID(id string) bool {
	if len(id) == 0 || len(id) > q.maxIDBytes {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return q.validateBatch(b)
}
