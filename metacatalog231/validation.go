package metacatalog231

// validateName enforces the structural name rules: non-empty, at most
// MaxNameBytes bytes, and only ASCII lowercase letters, digits, '-' and '_'.
// It performs no state reads and has no side effects.
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

// validateBatch performs complete structural validation of a batch without
// reading or mutating any catalog state. It is the single source of
// structural semantics shared by Apply and ValidateBatch.
func (s *Store) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if err := s.validateName(op.Name); err != nil {
			return err
		}
		switch op.Kind {
		case Put:
			if len(op.Value) == 0 || len(op.Value) > s.opts.MaxValueBytes {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	return s.validateBatch(b)
}
