package balanceledger227

import "math"

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error { return validateBatch(l.opts, b) }

// validateBatch is the single structural-validation routine shared by
// ValidateBatch and Apply. It never touches ledger state.
func validateBatch(o Options, b Batch) error {
	for _, op := range b.Ops {
		if err := validateName(o, op.Name); err != nil {
			return err
		}
		switch op.Kind {
		case Add:
			if op.Value != 0 || op.Delta == 0 {
				return ErrInvalidInput
			}
			if op.Delta == math.MinInt64 || abs64(op.Delta) > o.MaxAbsValue {
				return ErrValue
			}
		case Set:
			if op.Delta != 0 {
				return ErrInvalidInput
			}
			if op.Value == math.MinInt64 || abs64(op.Value) > o.MaxAbsValue {
				return ErrValue
			}
		case Delete:
			if op.Delta != 0 || op.Value != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validateName(o Options, name string) error {
	if len(name) == 0 || len(name) > o.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// addChecked computes base+delta (base is 0 when the account does not exist)
// detecting int64 overflow before the arithmetic and enforcing the absolute
// value limit on the result.
func addChecked(base, delta int64, exists bool, maxAbs int64) (int64, error) {
	if !exists {
		base = 0
	}
	if delta > 0 && base > math.MaxInt64-delta {
		return 0, ErrValue
	}
	if delta < 0 && base < math.MinInt64-delta {
		return 0, ErrValue
	}
	nv := base + delta
	if nv == math.MinInt64 || abs64(nv) > maxAbs {
		return 0, ErrValue
	}
	return nv, nil
}
