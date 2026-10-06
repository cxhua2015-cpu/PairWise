package expirytable289

// ValidateBatch performs complete structural validation without reading or
// mutating state, then checks the time precondition. It shares the exact
// structural semantics used by Apply.
func (t *Table) ValidateBatch(b Batch) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.validateBatchLocked(b); err != nil {
		return err
	}
	if b.Now < 0 || b.Now < t.now {
		return ErrTime
	}
	return nil
}

// validateBatchLocked checks structural rules only: known kinds, key
// alphabet and length limits, and Put/Touch expiry strictly after Now.
// The caller must hold t.mu; no state is read beyond limits or mutated.
func (t *Table) validateBatchLocked(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt <= b.Now {
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(k string, maxBytes int) bool {
	if len(k) == 0 || len(k) > maxBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
