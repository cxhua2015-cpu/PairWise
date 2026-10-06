package expirytable264

// ValidateBatch performs complete structural validation without reading or
// mutating table state. It shares the exact structural semantics used by
// Apply: non-negative batch time, known op kinds, well-formed keys within
// the configured byte limit, and Put/Touch expirations strictly in the
// future relative to the batch clock.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if err := validateKey(op.Key, t.maxKeyBytes); err != nil {
				return err
			}
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if err := validateKey(op.Key, t.maxKeyBytes); err != nil {
				return err
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateKey accepts only non-empty ASCII lowercase letters, digits,
// hyphens and underscores, bounded by maxBytes.
func validateKey(key string, maxBytes int) error {
	if key == "" || len(key) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
