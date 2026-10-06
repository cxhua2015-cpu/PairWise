package topologygraph258

// validName reports whether s is a non-empty ASCII name made of lowercase
// letters, digits, hyphens and underscores, within the byte limit.
func validName(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes {
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

// validateOp checks the structural shape of a single op without reading state.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !validName(op.From, maxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, maxNameBytes) || !validName(op.To, maxNameBytes) {
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics used by Apply.
func (g *Graph) ValidateBatch(b Batch) error {
	g.mu.RLock()
	maxNameBytes := g.nameBytes
	g.mu.RUnlock()
	for _, op := range b.Ops {
		if err := validateOp(op, maxNameBytes); err != nil {
			return err
		}
	}
	return nil
}
