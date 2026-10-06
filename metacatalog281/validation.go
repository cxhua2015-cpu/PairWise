package metacatalog281

// validateName enforces the shared name grammar: non-empty, at most
// maxBytes long, and limited to ASCII lowercase letters, digits,
// hyphens and underscores.
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply:
// known kinds, valid names, a non-empty in-limit value for Put, and a nil
// value for Delete. Capacity and existence are deliberately not checked
// here because they depend on store state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if err := validateName(op.Name, s.opts.MaxNameBytes); err != nil {
				return err
			}
			if len(op.Value) == 0 || len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if err := validateName(op.Name, s.opts.MaxNameBytes); err != nil {
				return err
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
