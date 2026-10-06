package balanceledger242

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares these exact semantics before opening a
// candidate transaction.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := l.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (l *Ledger) validateOp(op Op) error {
	switch op.Kind {
	case Add:
		if op.Value != 0 || op.Delta == 0 {
			return ErrInvalidInput
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
