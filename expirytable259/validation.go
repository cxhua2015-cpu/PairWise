package expirytable259

// validateBatch performs complete structural validation of a batch without
// reading or mutating table state. It is shared by ValidateBatch and Apply so
// both enforce identical structural semantics.
func validateBatch(b Batch, maxKeyBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, maxKeyBytes) {
			return ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(k string, maxKeyBytes int) bool {
	if len(k) == 0 || len(k) > maxKeyBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	return validateBatch(b, t.maxKeyBytes)
}
