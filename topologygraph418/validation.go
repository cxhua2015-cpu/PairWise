package topologygraph418

// validName reports whether s is a non-empty ASCII name of at most maxBytes
// bytes, using only lowercase letters, digits, hyphens and underscores.
func validName(s string, maxBytes int) bool {
	if s == "" || len(s) > maxBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
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
// mutating state. It shares the exact structural semantics used by Apply.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

// validateOp checks one op structurally: known kind, required fields present
// with valid names, and no extra fields set for the kind.
func (g *Graph) validateOp(op Op) error {
	max := g.opts.MaxNameBytes
	switch op.Kind {
	case AddNode, DeleteNode:
		if !validName(op.From, max) {
			return ErrInvalidInput
		}
		if op.To != "" {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, max) || !validName(op.To, max) {
			return ErrInvalidInput
		}
		if op.From == op.To {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}
