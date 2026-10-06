package expirytable224

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error { return validateBatch(t.opts, b) }

// validateBatch is the single structural semantics shared by Apply and ValidateBatch.
func validateBatch(o Options, b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(o, op.Key) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(o Options, k string) bool {
	if len(k) == 0 || len(k) > o.MaxKeyBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
