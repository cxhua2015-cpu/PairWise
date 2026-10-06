package expirytable294

// ValidateBatch performs complete structural validation without reading or mutating state.
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

func validKey(k string, max int) bool {
	if len(k) == 0 || len(k) > max {
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
