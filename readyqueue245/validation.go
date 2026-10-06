package readyqueue245

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatch(b, q.opts)
}

// validateBatch is the shared structural semantics used by both
// ValidateBatch (side-effect-free precheck) and Apply (candidate transaction).
func validateBatch(b Batch, opts Options) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if err := validateID(op.ID, opts.MaxIDBytes); err != nil {
				return err
			}
			if op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			if err := validateID(op.ID, opts.MaxIDBytes); err != nil {
				return err
			}
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func validateID(id string, maxBytes int) error {
	if len(id) == 0 || len(id) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return ErrInvalidInput
		}
	}
	return nil
}
