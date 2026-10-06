package balanceledger297

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics enforced by Apply: known kinds,
// well-formed names, no unexpected payload fields, and non-zero Add deltas.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add:
			if op.Value != 0 || op.Delta == 0 {
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
	}
	return nil
}

// validName reports whether s is a non-empty ASCII name of at most max bytes
// using only lowercase letters, digits, hyphens, and underscores.
func validName(s string, max int) bool {
	if s == "" || len(s) > max {
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
