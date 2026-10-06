package metacatalog256

// validateName enforces the structural name rules: non-empty ASCII
// lowercase letters, digits, hyphens and underscores, bounded by
// Options.MaxNameBytes. It never reads store state.
func (s *Store) validateName(name string) error {
	if len(name) == 0 || len(name) > s.opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateOp enforces the structural rules of a single operation
// without reading or mutating store state.
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
	return s.validateName(op.Name)
}

// ValidateBatch performs complete structural validation without reading
// or mutating state. Apply shares this exact preflight so both entry
// points observe identical structural semantics.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
