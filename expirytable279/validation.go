package expirytable279

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, t.maxKeyBytes) {
				return ErrInvalidInput
			}
			// An entry that would already be expired at the batch's
			// logical time is structurally invalid.
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, t.maxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether key is a non-empty ASCII string of lowercase
// letters, digits, hyphens and underscores within the byte limit.
func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
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
