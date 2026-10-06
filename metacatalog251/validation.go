package metacatalog251

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

func (s *Store) validateOp(op Op) error {
	if op.Kind != Put && op.Kind != Delete {
		return ErrInvalidInput
	}
	if err := validateName(op.Name, s.opts.MaxNameBytes); err != nil {
		return err
	}
	if op.Kind == Delete && op.Value != nil {
		return ErrInvalidInput
	}
	if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
		return ErrInvalidInput
	}
	return nil
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
