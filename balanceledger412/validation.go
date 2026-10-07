package balanceledger412

// validateOptions enforces that all capacity and length limits are positive.
func validateOptions(o Options) error {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return ErrInvalidOptions
	}
	return nil
}

// validateName reports whether a name is a non-empty ASCII identifier of
// lowercase letters, digits, hyphens and underscores within the byte limit.
func validateName(name string, maxBytes int) error {
	if name == "" || len(name) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateOp checks the structural shape of a single op without any state.
func validateOp(op Op, opts Options) error {
	switch op.Kind {
	case Add:
		if op.Delta == 0 {
			return ErrInvalidInput
		}
	case Set:
		if op.Value == 0 {
			return ErrInvalidInput
		}
	case Delete:
	default:
		return ErrInvalidInput
	}
	return validateName(op.Name, opts.MaxNameBytes)
}

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. It shares the exact structural semantics of Apply.
func (l *Ledger) ValidateBatch(b Batch) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.validateBatchLocked(b)
}

// validateBatchLocked is the shared structural preflight used by both
// ValidateBatch and Apply. Callers must hold at least the read lock.
func (l *Ledger) validateBatchLocked(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, l.opts); err != nil {
			return err
		}
	}
	return nil
}
