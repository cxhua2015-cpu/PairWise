package metacatalog276

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := s.opts.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (opts Options) validateOp(op Op) error {
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
	return opts.validateName(op.Name)
}

func (opts Options) validateName(name string) error {
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
