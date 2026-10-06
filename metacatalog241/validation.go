package metacatalog241

// validateName checks the structural name rules shared by Apply,
// ValidateBatch and Get: non-empty, ASCII lowercase letters, digits,
// hyphen and underscore, within the configured byte limit.
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

// validateOp checks one op structurally without reading store state.
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

// validateBatch is the shared structural preflight used by Apply and
// ValidateBatch. It never reads or mutates store state.
func (s *Store) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	return s.validateBatch(b)
}
