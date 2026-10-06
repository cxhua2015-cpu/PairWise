package metacatalog226

// ValidateBatch performs complete structural validation without reading or
// mutating store state. It shares the exact structural semantics used by
// Apply before any state access.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if err := s.validateName(op.Name); err != nil {
			return err
		}
		switch op.Kind {
		case Put:
			if op.Value == nil || len(op.Value) > s.opts.MaxValueBytes {
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

// validateName enforces the name grammar: non-empty ASCII lowercase letters,
// digits, hyphens and underscores, bounded by MaxNameBytes.
func (s *Store) validateName(name string) error {
	if len(name) == 0 || len(name) > s.opts.MaxNameBytes {
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
