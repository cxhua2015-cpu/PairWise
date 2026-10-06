package metacatalog271

func validateName(name string, maxNameBytes int) error {
	if len(name) == 0 || len(name) > maxNameBytes {
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

// validateOp checks a single op structurally against the options.
func validateOp(op Op, opts Options) error {
	switch op.Kind {
	case Put:
		if err := validateName(op.Name, opts.MaxNameBytes); err != nil {
			return err
		}
		if len(op.Value) > opts.MaxValueBytes {
			return ErrInvalidInput
		}
		return nil
	case Delete:
		if err := validateName(op.Name, opts.MaxNameBytes); err != nil {
			return err
		}
		if op.Value != nil {
			return ErrInvalidInput
		}
		return nil
	default:
		return ErrInvalidInput
	}
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, s.opts); err != nil {
			return err
		}
	}
	return nil
}
