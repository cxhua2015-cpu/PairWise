package expirytable259

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics enforced by Apply.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := t.validateOp(op, b.Now); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) validateOp(op Op, now int64) error {
	switch op.Kind {
	case Put, Touch:
		if err := t.validateKey(op.Key); err != nil {
			return err
		}
		if op.ExpiresAt <= now {
			return ErrInvalidInput
		}
	case Delete:
		if err := t.validateKey(op.Key); err != nil {
			return err
		}
		if op.ExpiresAt != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (t *Table) validateKey(key string) error {
	if len(key) == 0 || len(key) > t.maxKeyBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
