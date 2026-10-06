package expirytable239

// ValidateBatch performs complete structural validation without reading
// or mutating state. It shares its semantics with Apply: a batch that
// fails here would fail Apply with the same error before any time check.
func (t *Table) ValidateBatch(b Batch) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.validateBatch(b)
}

// validateBatch checks structural rules only; it never inspects entries,
// the clock, or any other mutable state. Callers must hold no lock or
// hold t.mu; it is side-effect free either way.
func (t *Table) validateBatch(b Batch) error {
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
	if !validKey(op.Key, t.maxKeyBytes) {
		return ErrInvalidInput
	}
	if op.Kind != Delete && op.ExpiresAt <= now {
		return ErrInvalidInput
	}
	return nil
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
