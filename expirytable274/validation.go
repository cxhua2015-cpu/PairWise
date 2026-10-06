package expirytable274

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Touch && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.opts.MaxKeyBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Delete {
			// Delete carries no payload; a non-zero ExpiresAt is an extra field.
			if op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
			continue
		}
		// Put/Touch must outlive the batch clock or they would expire immediately.
		if op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
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
