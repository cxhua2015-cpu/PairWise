package expirytable244

// validateBatchStruct performs complete structural validation of a batch
// without reading or mutating any table state. It is the single source of
// structural semantics shared by Apply and ValidateBatch.
func validateBatchStruct(b Batch, maxKeyBytes int) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, maxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validKey reports whether s is a non-empty ASCII key of at most max bytes
// using only lowercase letters, digits, hyphens and underscores.
func validKey(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	return validateBatchStruct(b, t.maxKeyBytes)
}
