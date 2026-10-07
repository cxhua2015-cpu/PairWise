package expirytable429

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	return t.validateBatch(b)
}

// validateBatch checks the batch structurally: non-negative time, known op
// kinds, key shape and length limits, and ExpiresAt > Now for Put/Touch.
// It shares its semantics with Apply and never touches table state.
func (t *Table) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
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

// validKey reports whether key is non-empty, fits the byte limit, and
// contains only ASCII lowercase letters, digits, hyphens, and underscores.
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
