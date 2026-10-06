package expirytable234

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	return validateBatchShape(b, t.maxKeyBytes)
}

// validateBatchShape is the single structural-validation routine shared by
// ValidateBatch and Apply. It never touches table state.
func (t *Table) validateBatch(b Batch) error {
	return validateBatchShape(b, t.maxKeyBytes)
}

func validateBatchShape(b Batch, maxKeyBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
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

// validKey reports whether key is non-empty, within the byte limit, and
// consists only of ASCII lowercase letters, digits, hyphens, and underscores.
func validKey(key string, maxKeyBytes int) bool {
	if len(key) == 0 || len(key) > maxKeyBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
