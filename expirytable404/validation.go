package expirytable404

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := t.validateOp(b.Now, op); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) validateOp(now int64, op Op) error {
	switch op.Kind {
	case Put, Touch, Delete:
	default:
		return ErrInvalidInput
	}
	if !validKey(op.Key) || len(op.Key) > t.maxKeyBytes {
		return ErrInvalidInput
	}
	if op.Kind != Delete && op.ExpiresAt <= now {
		return ErrInvalidInput
	}
	return nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
