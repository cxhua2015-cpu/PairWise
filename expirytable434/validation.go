package expirytable434

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares its structural semantics with Apply: Now must be non-negative,
// every op must carry a known kind and a legal key, and Put/Touch must
// expire strictly after the batch time. It never inspects table state.
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
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether key is non-empty, fits the byte budget and
// consists solely of ASCII lowercase letters, digits, '-' and '_'.
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
