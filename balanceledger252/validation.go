package balanceledger252

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return ErrInvalidInput
		}
		if err := l.validateName(op.Name); err != nil {
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

func (l *Ledger) validateName(name string) error {
	if len(name) == 0 || len(name) > l.opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return ErrInvalidInput
		}
	}
	return nil
}
