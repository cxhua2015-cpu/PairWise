package expirytable284

// ValidateBatch performs complete structural validation without reading or
// mutating table state. Apply shares these exact structural semantics.
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
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, t.maxKeyBytes) {
				return ErrInvalidInput
			}
			if op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether k is a non-empty ASCII lowercase
// letter/digit/hyphen/underscore string within the byte limit.
func validKey(k string, maxBytes int) bool {
	if k == "" || len(k) > maxBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
