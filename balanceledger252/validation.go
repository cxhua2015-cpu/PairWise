package balanceledger252

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply shares these exact semantics via this helper.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateName(op.Name, l.opts.MaxNameBytes); err != nil {
			return err
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 || op.Value != 0 {
				return ErrInvalidInput
			}
			if exceedsAbs(op.Delta, l.opts.MaxAbsValue) {
				return ErrValue
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if exceedsAbs(op.Value, l.opts.MaxAbsValue) {
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

// exceedsAbs reports whether |v| > limit without overflowing on MinInt64.
func exceedsAbs(v, limit int64) bool {
	return v > limit || v < -limit
}
