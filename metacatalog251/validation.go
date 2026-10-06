package metacatalog251

// ValidateBatch performs complete structural validation without reading or
// mutating any store state. It shares the exact structural semantics used
// by Apply: unknown kinds, invalid names, oversized values and non-nil
// Delete payloads are rejected with ErrInvalidInput.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if !validName(op.Name, s.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
			if len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if !validName(op.Name, s.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
			if op.Value != nil {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validName reports whether name is non-empty, fits the byte limit and
// consists only of ASCII lowercase letters, digits, hyphens and underscores.
func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
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
