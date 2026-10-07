package expirytable409

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply: non-negative
// time, known op kinds, well-formed keys within the configured byte
// limit, and Put/Touch deadlines strictly beyond the batch time.
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
		if op.Kind == Put || op.Kind == Touch {
			if op.ExpiresAt < 0 || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
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
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
