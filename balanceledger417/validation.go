package balanceledger417

func validName(o Options, name string) bool {
	if name == "" || len(name) > o.MaxNameBytes {
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

// validateBatch is the shared structural preflight used by both
// ValidateBatch and Apply. It never reads or mutates ledger state.
func validateBatch(o Options, b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Add, Set, Delete:
		default:
			return ErrInvalidInput
		}
		if !validName(o, op.Name) {
			return ErrInvalidInput
		}
		if op.Kind == Add && op.Delta == 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	return validateBatch(l.opts, b)
}
