package expirytable284

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error { return t.validateBatch(b) }

// validateBatch is the single structural semantics shared by Apply and
// ValidateBatch. It is pure: it never reads mutable table state.
func (t *Table) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if err := t.validateKey(op.Key); err != nil {
				return err
			}
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
