package readyqueue275

// validID reports whether id is a non-empty ASCII string of lowercase
// letters, digits, hyphens and underscores within the byte limit.
func (q *Queue) validID(id string) bool {
	if id == "" || len(id) > q.opts.MaxIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateBatchLocked is the single source of structural semantics,
// shared by Apply and ValidateBatch. It is side-effect free: it never
// reads or mutates queue state beyond the immutable options.
func (q *Queue) validateBatchLocked(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if !q.validID(op.ID) || op.ReadyAt < 0 {
				return ErrInvalidInput
			}
		case Cancel:
			if !q.validID(op.ID) || op.Priority != 0 || op.ReadyAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (q *Queue) ValidateBatch(b Batch) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.validateBatchLocked(b)
}
