package readyqueue240

// validID reports whether id is a non-empty ASCII identifier of at most
// maxBytes bytes, using only lowercase letters, digits, '-' and '_'.
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

// validateBatchStructure performs complete structural validation of a batch
// without reading or mutating any queue state. Unknown kinds, malformed IDs,
// negative timestamps, and fields set for kinds that do not use them are all
// rejected with ErrInvalidInput.
func validateBatchStructure(opts Options, b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
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
		if !validID(op.ID, opts.MaxIDBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStructure(q.opts, b)
}
