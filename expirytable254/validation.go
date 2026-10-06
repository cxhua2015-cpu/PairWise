package expirytable254

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply before any time check.
func (t *Table) ValidateBatch(b Batch) error {
	return validateBatch(b, t.maxKeyBytes)
}

func validateBatch(b Batch, maxKeyBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
			// An entry expiring at or before Now would be swept by the
			// candidate pre-pass, so it is structurally meaningless.
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(key string, maxKeyBytes int) bool {
	if len(key) == 0 || len(key) > maxKeyBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
