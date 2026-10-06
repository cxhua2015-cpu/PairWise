package expirytable244

// ValidateBatch performs complete structural validation without reading or
// mutating table state. Apply shares this exact pre-check so both entry
// points agree on what a well-formed batch is.
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
	case Put, Touch, Delete:
	default:
		return ErrInvalidInput
	}
	if !validKey(op.Key) || len(op.Key) > t.maxKeyBytes {
		return ErrInvalidInput
	}
	if op.Kind == Delete {
		return nil
	}
	// A Put/Touch must outlive the sweep at Now (closed interval), so an
	// expiry at or before Now can never be observed and is rejected.
	if op.ExpiresAt <= now {
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
