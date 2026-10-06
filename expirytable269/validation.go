package expirytable269

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact semantics before touching the
// clock or the index.
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
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
		// Put/Touch must outlive the batch clock, otherwise the entry
		// would be born already expired under the closed-interval rule.
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether key is non-empty ASCII lowercase letters,
// digits, hyphens and underscores, within the configured byte limit.
func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
