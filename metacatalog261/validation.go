package metacatalog261

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return validateBatch(s.opts, b)
}

func validateBatch(opts Options, b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if err := validateName(opts, op.Name); err != nil {
			return err
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > opts.MaxValueBytes {
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
