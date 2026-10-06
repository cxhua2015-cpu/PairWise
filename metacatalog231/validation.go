package metacatalog231

// validateName reports whether name is a non-empty ASCII identifier of
// lowercase letters, digits, hyphens and underscores within maxBytes.
func validateName(name string, maxBytes int) error {
	if len(name) == 0 || len(name) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

// validateOp checks a single op structurally, without touching store state.
func validateOp(op Op, opts Options) error {
	switch op.Kind {
	case Put:
		if op.Value == nil || len(op.Value) > opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validateName(op.Name, opts.MaxNameBytes)
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, s.opts); err != nil {
			return err
		}
	}
	return nil
}
