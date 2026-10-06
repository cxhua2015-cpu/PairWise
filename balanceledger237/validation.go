package balanceledger237

// ValidateBatch performs complete structural validation without reading or
// mutating ledger state. Apply runs the exact same checks before touching
// state, so both entry points share one structural semantics.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
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
		if err := l.checkName(op.Name); err != nil {
			return err
		}
	}
	return nil
}

// checkName enforces the name alphabet and the configured byte limit. It only
// reads l.opts, which is immutable after New, so it needs no lock.
func (l *Ledger) checkName(name string) error {
	if len(name) == 0 || len(name) > l.opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
