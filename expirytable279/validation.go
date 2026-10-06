package expirytable279

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply:
// non-negative time, known op kinds, and well-formed keys within the
// configured byte limit. Put/Touch additionally require ExpiresAt to be
// strictly greater than the batch's Now.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	t.mu.Lock()
	maxKeyBytes := t.maxKeyBytes
	t.mu.Unlock()
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, maxKeyBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether key is non-empty, within maxBytes, and composed
// only of ASCII lowercase letters, digits, hyphens, and underscores.
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
