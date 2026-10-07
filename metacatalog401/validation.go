package metacatalog401

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, s.opts); err != nil {
			return err
		}
	}
	return nil
}

func validateOp(op Op, o Options) error {
	switch op.Kind {
	case Put:
		if op.Value == nil {
			return ErrInvalidInput
		}
		if len(op.Value) > o.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validateName(op.Name, o.MaxNameBytes)
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
