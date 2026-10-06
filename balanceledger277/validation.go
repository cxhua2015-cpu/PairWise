package balanceledger277

func validateOptions(o Options) error {
	if o.MaxAccounts <= 0 || o.MaxNameBytes <= 0 || o.MaxAbsValue <= 0 {
		return ErrInvalidOptions
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

func validAbs(v, maxAbs int64) bool {
	return v >= -maxAbs && v <= maxAbs
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (l *Ledger) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if !validName(op.Name, l.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		switch op.Kind {
		case Add:
			if op.Delta == 0 || op.Value != 0 || !validAbs(op.Delta, l.opts.MaxAbsValue) {
				return ErrInvalidInput
			}
		case Set:
			if op.Delta != 0 || !validAbs(op.Value, l.opts.MaxAbsValue) {
				return ErrInvalidInput
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
