package balanceledger287

// validName reports whether name is a non-empty ASCII identifier of
// lowercase letters, digits, hyphens and underscores within maxBytes.
func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 {
				return ErrInvalidInput
			}
			if op.Delta == 0 {
				return ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if !withinAbs(op.Value, l.opts.MaxAbsValue) {
				return ErrValue
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
	}
	return nil
}
