package readyqueue255

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. It shares the exact structural semantics of Apply.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatchStruct(b, q.opts.MaxIDBytes)
}

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

func validateBatchStruct(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrTime
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
		if !validID(op.ID, maxIDBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}
