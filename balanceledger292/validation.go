package balanceledger292

// ValidateBatch performs complete structural validation without reading or mutating state.
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
	if !validName(op.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	return nil
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
