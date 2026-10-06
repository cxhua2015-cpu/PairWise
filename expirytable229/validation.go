package expirytable229

// validKey reports whether k is a non-empty ASCII key of lowercase letters,
// digits, hyphens and underscores within the configured byte limit.
func (t *Table) validKey(k string) bool {
	if len(k) == 0 || len(k) > t.maxKeyBytes {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !t.validKey(op.Key) {
				return ErrInvalidInput
			}
			// A Put/Touch must outlive the batch clock; an entry expiring
			// at or before Now would be dead on arrival.
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !t.validKey(op.Key) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}
