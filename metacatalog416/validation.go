package metacatalog416

// validName reports whether name is non-empty, within the byte limit, and
// consists only of ASCII lowercase letters, digits, '-' and '_'.
func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if op.Value != nil {
				return ErrInvalidInput
			}
		}
	}
	return nil
}
