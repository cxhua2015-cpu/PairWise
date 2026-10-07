package metacatalog436

// validateBatch performs complete structural validation without reading or
// mutating any store state. It is the single source of structural semantics
// shared by Apply, ValidateBatch and Preview.
func validateBatch(opts Options, b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if err := validateName(opts, op.Name); err != nil {
				return err
			}
			if len(op.Value) > opts.MaxValueBytes {
				return ErrInvalidInput
			}
		case Delete:
			if err := validateName(opts, op.Name); err != nil {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	return validateBatch(s.opts, b)
}
