package balanceledger432

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares exactly these structural semantics:
// a batch that fails ValidateBatch fails Apply with the same error before
// any account state is consulted.
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
		// Add carries only Delta; a non-zero Value is an extra field.
		if op.Value != 0 || op.Delta == 0 || !withinAbs(op.Delta, l.opts.MaxAbsValue) {
			return ErrInvalidInput
		}
	case Set:
		// Set carries only Value; a non-zero Delta is an extra field.
		if op.Delta != 0 || !withinAbs(op.Value, l.opts.MaxAbsValue) {
			return ErrInvalidInput
		}
	case Delete:
		// Delete carries neither Delta nor Value.
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

func withinAbs(v, limit int64) bool {
	// limit > 0 is guaranteed by New; v == math.MinInt64 has -v overflow,
	// so compare without negation.
	return v <= limit && v >= -limit
}
