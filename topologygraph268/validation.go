package topologygraph268

// validName reports whether n is a non-empty ASCII name of at most max bytes
// consisting only of lowercase letters, digits, hyphens and underscores.
func validName(n string, max int) bool {
	if n == "" || len(n) > max {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp checks the structural shape of a single op without reading state.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" || !validName(op.From, maxNameBytes) {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := validateOp(op, g.opts.MaxNameBytes); err != nil {
			return err
		}
	}
	return nil
}
