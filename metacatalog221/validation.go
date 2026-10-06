package metacatalog221

// validName enforces the shared structural rule: non-empty ASCII
// lowercase letters, digits, hyphen and underscore, within maxBytes.
func validName(name string, maxBytes int) error {
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

// validateOp checks one op structurally without reading any state.
func (s *Store) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validName(op.Name, s.opts.MaxNameBytes)
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
