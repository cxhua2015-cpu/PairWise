package metacatalog401

// validateName enforces the shared structural rules for names and keys:
// non-empty, at most MaxNameBytes bytes, ASCII lowercase letters, digits,
// hyphens, and underscores only.
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact semantics before touching the
// store. Unknown kinds, malformed names, oversized values, and fields that
// are not allowed for the op kind all yield ErrInvalidInput.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if err := s.validateName(op.Name); err != nil {
				return err
			}
			if op.Value == nil || len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if err := s.validateName(op.Name); err != nil {
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
