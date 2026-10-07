package readyqueue420

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	return validateBatch(&q.opts, b)
}

// validateBatch is the single structural-validation routine shared by
// ValidateBatch and Apply. It never touches queue state.
func validateBatch(opts *Options, b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if err := validateID(opts, op.ID); err != nil {
			return err
		}
		switch op.Kind {
		case Enqueue:
			if op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			if op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

// validateID enforces non-empty ASCII [a-z0-9-_] IDs within MaxIDBytes.
func validateID(opts *Options, id string) error {
	if len(id) == 0 || len(id) > opts.MaxIDBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
