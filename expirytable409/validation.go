package expirytable409

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares the same structural semantics.
func (t *Table) ValidateBatch(b Batch) error {
	return t.validateBatch(b)
}

// validateBatch is side-effect free: it only consults immutable options.
func (t *Table) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if err := t.validateKey(op.Key); err != nil {
			return err
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
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
