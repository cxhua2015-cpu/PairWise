package balanceledger267

// validName reports whether name satisfies the structural rules:
// non-empty ASCII lowercase letters, digits, hyphens and underscores,
// bounded by the configured byte limit.
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
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Add && op.Delta == 0 {
			return ErrInvalidInput
		}
	}
	return nil
}
