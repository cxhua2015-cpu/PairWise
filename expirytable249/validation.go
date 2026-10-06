package expirytable249

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	return validateBatch(t.maxKeyBytes, b)
}

// validateBatch is the single structural semantics shared by ValidateBatch and
// Apply. It is pure: it never reads table state and never mutates anything.
func validateBatch(maxKeyBytes int, b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
			// An entry expiring at or before Now would be dead on arrival.
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
			// Delete carries no extra fields; ExpiresAt must be unset.
			if op.ExpiresAt != 0 {
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
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
