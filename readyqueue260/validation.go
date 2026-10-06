package readyqueue260

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. It shares the exact structural semantics used by
// Apply before any state is touched.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := q.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue) validateOp(op Op) error {
	switch op.Kind {
	case Enqueue:
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	case Cancel:
		if op.Priority != 0 || op.ReadyAt != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return q.validateID(op.ID)
}

func (q *Queue) validateID(id string) error {
	if len(id) == 0 || len(id) > q.opts.MaxIDBytes {
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
