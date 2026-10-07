package metacatalog431

// validateName enforces the structural name rules: non-empty, at most
// MaxNameBytes bytes, ASCII lowercase letters, digits, '-' and '_'.
func validateName(opts Options, name string) error {
	if len(name) == 0 || len(name) > opts.MaxNameBytes {
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
func validateOp(opts Options, op Op) error {
	switch op.Kind {
	case Put:
		if len(op.Value) > opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validateName(opts, op.Name)
}

// validateBatch performs complete structural validation of the whole batch
// without reading or mutating state. Apply shares this exact semantics.
func validateBatch(opts Options, b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(opts, op); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	s.mu.RLock()
	opts := s.opts
	s.mu.RUnlock()
	return validateBatch(opts, b)
}
