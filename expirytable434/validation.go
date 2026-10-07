package expirytable434

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateBatch performs complete structural validation without reading state.
func validateBatch(opts Options, b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, opts.MaxKeyBytes) || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, opts.MaxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (t *Table) ValidateBatch(b Batch) error {
	t.mu.Lock()
	opts := t.opts
	t.mu.Unlock()
	return validateBatch(opts, b)
}
