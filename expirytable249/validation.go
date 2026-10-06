package expirytable249

// validateOptions enforces that capacity and length limits are positive.
func validateOptions(o Options) error {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return ErrInvalidOptions
	}
	return nil
}

// validKey reports whether key is a non-empty ASCII string of lowercase
// letters, digits, hyphens and underscores within the byte limit.
func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply:
// non-negative time, known kinds, valid keys, and Put/Touch expiries that
// would not be immediately evicted at the batch's own Now.
func (t *Table) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, t.opts.MaxKeyBytes) || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, t.opts.MaxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}
