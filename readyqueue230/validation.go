package readyqueue230

// validID reports whether id is a non-empty ASCII key of at most maxBytes
// bytes using only lowercase letters, digits, hyphens and underscores.
func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
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

// ValidateBatch performs complete structural validation without reading or
// mutating queue state. Apply shares these exact semantics: a batch that
// fails here is rejected before any state is touched.
func (q *Queue) ValidateBatch(b Batch) error {
	q.mu.Lock()
	maxID := q.opts.MaxIDBytes
	q.mu.Unlock()
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validID(op.ID, maxID) {
			return ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
		// Cancel carries no payload; Priority and ReadyAt are extra
		// fields and must be zero.
		if op.Kind == Cancel && (op.Priority != 0 || op.ReadyAt != 0) {
			return ErrInvalidInput
		}
	}
	return nil
}
