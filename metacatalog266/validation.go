package metacatalog266

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := s.opts.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (o Options) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if err := o.validateName(op.Name); err != nil {
			return err
		}
		if len(op.Value) > o.MaxValueBytes {
			return ErrInvalidInput
		}
		return nil
	case Delete:
		if err := o.validateName(op.Name); err != nil {
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

func (o Options) validateName(name string) error {
	if len(name) == 0 || len(name) > o.MaxNameBytes {
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
