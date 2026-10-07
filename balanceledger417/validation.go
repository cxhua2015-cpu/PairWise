package balanceledger417

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares exactly these structural semantics, so a
// batch rejected here is rejected by Apply before any state is observed.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			// Delta carries the operand; Value is an extra field here.
			if op.Delta == 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		case Set:
			// Value carries the operand; Delta is an extra field here.
			if op.Value == 0 || op.Delta != 0 {
				return ErrInvalidInput
			}
			// Absolute-value limit is a structural property of the operand.
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
	}
	return nil
}

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
