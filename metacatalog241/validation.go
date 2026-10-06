package metacatalog241

// ValidateBatch performs complete structural validation without reading or
// mutating store state. Apply shares this exact structural preflight so both
// entry points reject the same malformed batches.
func (s *Store) ValidateBatch(b Batch) error {
	return validateBatch(b, s.opts)
}

func validateBatch(b Batch, opts Options) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if err := validateName(op.Name, opts.MaxNameBytes); err != nil {
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
