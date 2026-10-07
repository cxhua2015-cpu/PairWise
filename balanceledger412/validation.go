package balanceledger412

// validName reports whether name is non-empty ASCII [a-z0-9-_] within the
// configured byte limit.
func (l *Ledger) validName(name string) bool {
	if name == "" || len(name) > l.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares this exact preflight so committed
// transactions and ValidateBatch can never disagree on structure.
func (l *Ledger) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		if !l.validName(op.Name) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	return l.validateBatch(b)
}
