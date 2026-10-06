package topologygraph298

// validateName enforces the shared name grammar: non-empty ASCII
// lowercase letters, digits, hyphens and underscores, bounded in bytes.
func validateName(s string, maxBytes int) error {
	if len(s) == 0 || len(s) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

// validateOp checks one op structurally, without reading graph state.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		return validateName(op.From, maxNameBytes)
	case AddEdge, DeleteEdge:
		if err := validateName(op.From, maxNameBytes); err != nil {
			return err
		}
		if err := validateName(op.To, maxNameBytes); err != nil {
			return err
		}
		if op.From == op.To {
			return ErrInvalidInput
		}
		return nil
	default:
		return ErrInvalidInput
	}
}

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, g.opts.MaxNameBytes); err != nil {
			return err
		}
	}
	return nil
}
