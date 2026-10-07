package expirytable404

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (t *Table) ValidateBatch(b Batch) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.validate(b)
}

// validate checks structural rules only; callers handle time and state.
// The caller must hold at least a read lock so the limits are stable.
func (t *Table) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if err := t.validateKey(op.Key); err != nil {
				return err
			}
			// An entry that would already be expired at batch time is
			// structurally invalid.
			if op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if err := t.validateKey(op.Key); err != nil {
				return err
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) validateKey(k string) error {
	if len(k) == 0 || len(k) > t.maxKeyBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
