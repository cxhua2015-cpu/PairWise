package balanceledger237

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply: unknown kinds,
// unexpected populated fields, malformed names, and out-of-limit
// magnitudes are rejected before any state is touched.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := l.validateName(op.Name); err != nil {
			return err
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 || op.Value != 0 {
				return ErrInvalidInput
			}
			if absExceeds(op.Delta, l.opts.MaxAbsValue) {
				return ErrValue
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if absExceeds(op.Value, l.opts.MaxAbsValue) {
				return ErrValue
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

// validateName enforces the name alphabet and byte-length limit.
func (l *Ledger) validateName(name string) error {
	if name == "" || len(name) > l.opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
