package balanceledger227

// validateName enforces the structural name rules: non-empty, at most
// maxBytes bytes, limited to ASCII lowercase letters, digits, '-' and '_'.
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

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares exactly these structural semantics.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if err := validateName(op.Name, l.opts.MaxNameBytes); err != nil {
			return err
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 {
				return ErrInvalidInput
			}
			if op.Delta > l.opts.MaxAbsValue || op.Delta < -l.opts.MaxAbsValue {
				return ErrValue
			}
		case Set:
			if op.Value > l.opts.MaxAbsValue || op.Value < -l.opts.MaxAbsValue {
				return ErrValue
			}
		}
	}
	return nil
}
