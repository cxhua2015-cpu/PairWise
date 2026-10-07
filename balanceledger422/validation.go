package balanceledger422

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return validateBatch(l.opts, b)
}

func validateBatch(opts Options, b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(opts, op.Name) {
			return ErrInvalidInput
		}
		if op.Kind == Add && op.Delta == 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

func validName(opts Options, name string) bool {
	if len(name) == 0 || len(name) > opts.MaxNameBytes {
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
