package metacatalog236

// ValidateBatch performs complete structural validation without reading or mutating state.
func (s *Store) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return ErrInvalidInput
		}
		if !validName(op.Name, s.nameLimit()) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Put:
			if len(op.Value) > s.valueLimit() {
				return ErrInvalidInput
			}
		case Delete:
			if op.Value != nil {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Store) valueLimit() int { return s.opts.MaxValueBytes }
