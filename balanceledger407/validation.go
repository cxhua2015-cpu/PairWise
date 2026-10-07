package balanceledger407

// validateOptions enforces that capacity and length limits are positive.
func validateOptions(o Options) error {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return ErrInvalidOptions
	}
	return nil
}

// validateName reports whether a name is non-empty ASCII [a-z0-9-_] and fits
// the configured byte limit. It is pure and reads no ledger state.
func validateName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact semantics before touching state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validateName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Kind == Add && op.Delta == 0 {
			return ErrInvalidInput
		}
	}
	return nil
}
