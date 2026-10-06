package balanceledger297

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if !validName(op.Name, l.opts.MaxNameBytes) {
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
