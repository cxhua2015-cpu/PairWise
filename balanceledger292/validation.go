package balanceledger292

// validateBatch performs complete structural validation of a batch without
// reading or mutating ledger state. It is the single source of structural
// semantics shared by ValidateBatch and Apply.
func (l *Ledger) validateBatch(b Batch) error {
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

func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply runs the exact same checks before touching state.
func (l *Ledger) ValidateBatch(b Batch) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.validateBatch(b)
}
