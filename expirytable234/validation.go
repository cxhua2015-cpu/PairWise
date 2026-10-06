package expirytable234

// ValidateBatch performs complete structural validation without reading or
// mutating table state. It shares the exact structural semantics used by
// Apply before any time check.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Touch && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
		// A Put/Touch that would already be expired at the batch time is
		// structurally meaningless.
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(k string, maxBytes int) bool {
	if len(k) == 0 || len(k) > maxBytes {
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
