package balanceledger257

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares the same structural semantics by
// delegating to validateBatch before touching any state.
func (l *Ledger) ValidateBatch(b Batch) error {
	return l.validateBatch(b)
}

func (l *Ledger) validateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := l.validateName(op.Name); err != nil {
			return err
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

func (l *Ledger) validateName(name string) error {
	if len(name) == 0 || len(name) > l.opts.MaxNameBytes {
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
