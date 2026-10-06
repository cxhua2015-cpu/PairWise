package balanceledger222

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.validate(b)
}

// validate checks structural semantics shared by ValidateBatch and Apply.
// It never reads account state.
func (l *Ledger) validate(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Add && op.Kind != Set && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Value != 0 || op.Delta == 0 {
				return ErrInvalidInput
			}
			if !withinAbs(op.Delta, l.opts.MaxAbsValue) {
				return ErrValue
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
		}
	}
	return nil
}

func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
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

func withinAbs(v, limit int64) bool {
	if v == -1<<63 {
		return false
	}
	if v < 0 {
		v = -v
	}
	return v <= limit
}
