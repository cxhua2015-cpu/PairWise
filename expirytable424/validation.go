package expirytable424

func validKey(key string, maxKeyBytes int) bool {
	if key == "" || len(key) > maxKeyBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateBatch is the shared structural precheck used by Apply and
// ValidateBatch. It never reads or mutates table state.
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return validateBatch(b, t.maxKeyBytes)
}
