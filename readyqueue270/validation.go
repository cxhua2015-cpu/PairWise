package readyqueue270

// validID reports whether id is a non-empty ASCII identifier of at most
// max bytes, using only lowercase letters, digits, '-' and '_'.
func validID(id string, max int) bool {
	if len(id) == 0 || len(id) > max {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp checks one op against the structural rules shared by
// ValidateBatch and Apply.
func validateOp(op Op, maxIDBytes int) error {
	switch op.Kind {
	case Enqueue:
		if !validID(op.ID, maxIDBytes) || op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	case Cancel:
		if !validID(op.ID, maxIDBytes) || op.Priority != 0 || op.ReadyAt != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. Apply runs the exact same checks before touching
// state, so a batch rejected here can never partially apply.
func (q *Queue) ValidateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateOp(op, q.opts.MaxIDBytes); err != nil {
			return err
		}
	}
	return nil
}
