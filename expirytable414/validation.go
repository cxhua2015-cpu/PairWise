package expirytable414

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact semantics before touching the
// clock or any entry.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether key is a non-empty ASCII string of lowercase
// letters, digits, hyphens and underscores within the byte limit.
func validKey(key string, maxBytes int) bool {
	if key == "" || len(key) > maxBytes {
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
