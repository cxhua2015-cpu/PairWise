package readyqueue425

// ValidateBatch performs complete structural validation without reading or mutating state.
// It checks only the batch against the immutable Options, so it never
// inspects or changes queue state and never returns state-dependent errors
// such as ErrExists, ErrNotFound, ErrCapacity or ErrTime.
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
	if !validID(op.ID) || len(op.ID) > q.opts.MaxIDBytes {
		return ErrInvalidInput
	}
	return nil
}

// validID reports whether id is non-empty and consists only of ASCII
// lowercase letters, digits, hyphens and underscores.
func validID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
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
