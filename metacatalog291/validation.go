package metacatalog291

// validateName reports whether name satisfies the charset and length rules.
// Names are non-empty and limited to ASCII lowercase letters, digits,
// hyphens and underscores, within the configured byte budget.
func validateName(name string, o Options) error {
	if len(name) == 0 || len(name) > o.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateOp checks one op against the structural contract.
func validateOp(op Op, o Options) error {
	if err := validateName(op.Name, o); err != nil {
		return err
	}
	switch op.Kind {
	case Put:
		if len(op.Value) == 0 || len(op.Value) > o.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply,
// so a batch accepted here never fails Apply on structural grounds.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, s.opts); err != nil {
			return err
		}
	}
	return nil
}
