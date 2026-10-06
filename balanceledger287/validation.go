package balanceledger287

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	return l.validateBatch(b)
}

// validateBatch is the shared structural preflight used by Apply and ValidateBatch.
// It checks kinds, names, and per-op value limits only; it never inspects ledger state.
func (l *Ledger) validateBatch(b Batch) error {
	for _, op := range b.Ops {
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
		case Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
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
