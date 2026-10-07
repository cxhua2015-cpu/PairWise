package balanceledger407

// validateName enforces the structural name rules: non-empty, within the
// configured byte budget, and limited to ASCII lowercase letters, digits,
// hyphens and underscores.
func validateName(name string, maxBytes int) error {
	if name == "" || len(name) > maxBytes {
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

// validateOp checks a single op structurally: known kind, well-formed name,
// and only the fields meaningful for the kind populated. Absolute-value
// limits on the inputs are enforced here as well so Apply and ValidateBatch
// share identical structural semantics.
func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add:
		if op.Delta == 0 || op.Value != 0 {
			return ErrInvalidInput
		}
		if op.Delta > l.opts.MaxAbsValue || op.Delta < -l.opts.MaxAbsValue {
			return ErrValue
		}
	case Set:
		if op.Delta != 0 {
			return ErrInvalidInput
		}
		if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
			return ErrValue
		}
	case Delete:
		if op.Delta != 0 || op.Value != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return validateName(op.Name, l.opts.MaxNameBytes)
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
