package expirytable424

// ValidateBatch performs complete structural validation without reading or
// mutating table state. Structural checks precede all time/state checks and use
// the same semantics as Apply.
func (t *Table) ValidateBatch(b Batch) error {
	return ValidateBatch(t.opt, b)
}

// ValidateBatch is the side-effect-free, lock-free precheck shared by every
// transaction entry point. It verifies:
//   - Now is non-negative;
//   - keys are non-empty ASCII strings made only of lowercase letters, digits,
//     '-' and '_', and fit the Options byte limit;
//   - operation kinds are known (Put/Touch/Delete) with no extra payload;
//   - Put/Touch carry a non-negative expiry strictly after Now (Delete ignores
//     its expiry field).
func ValidateBatch(opt Options, b Batch) error {
	if b.Now < 0 {
		return ErrTime
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, opt.MaxKeyBytes) {
				return ErrInvalidInput
			}
			if op.ExpiresAt < 0 || op.ExpiresAt <= b.Now {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, opt.MaxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
