package topologygraph248

// validName reports whether name is a non-empty ASCII identifier of
// lowercase letters, digits, hyphens and underscores within maxBytes.
func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
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

// validateOp checks the structural rules of a single op without reading
// any graph state. Unknown kinds, invalid names, self loops and unexpected
// extra fields are rejected with ErrInvalidInput.
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
	for _, op := range b.Ops {
		if err := validateOp(op, g.opts.MaxNameBytes); err != nil {
			return err
		}
	}
	return nil
}
