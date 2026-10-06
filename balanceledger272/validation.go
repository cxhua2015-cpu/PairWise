package balanceledger272

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, o := range b.Ops {
		if err := l.validateOp(o); err != nil {
			return err
		}
	}
	return nil
}

// validateOp checks only structural constraints: kind, name shape, and
// operand magnitude. It never inspects ledger state.
func (l *Ledger) validateOp(o Op) error {
	switch o.Kind {
	case Add, Set, Delete:
	default:
		return ErrInvalidInput
	}
	if !validName(o.Name, l.opts.MaxNameBytes) {
		return ErrInvalidInput
	}
	switch o.Kind {
	case Add:
		if o.Delta == 0 {
			return ErrInvalidInput
		}
		if o.Delta > l.opts.MaxAbsValue || o.Delta < -l.opts.MaxAbsValue {
			return ErrValue
		}
	case Set:
		if o.Value > l.opts.MaxAbsValue || o.Value < -l.opts.MaxAbsValue {
			return ErrValue
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
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
