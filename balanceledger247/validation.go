package balanceledger247

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact structural semantics.
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
		// Extra fields must be zero; a no-op delta is invalid input.
		if op.Value != 0 || op.Delta == 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Delta != 0 {
			return ErrInvalidInput
		}
		if exceedsLimit(op.Value, l.opts.MaxAbsValue) {
			return ErrValue
		}
	case Delete:
		if op.Delta != 0 || op.Value != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return l.validateName(op.Name)
}

func (l *Ledger) validateName(name string) error {
	if name == "" || len(name) > l.opts.MaxNameBytes {
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
