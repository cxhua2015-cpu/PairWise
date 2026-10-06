package metacatalog286

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if err := validateName(op.Name, s.opts.MaxNameBytes); err != nil {
				return err
			}
			if len(op.Value) > s.opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if err := validateName(op.Name, s.opts.MaxNameBytes); err != nil {
				return err
			}
			if op.Value != nil {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
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
