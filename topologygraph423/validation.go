package topologygraph423

// validName reports whether n is a non-empty ASCII name within the byte limit,
// using only lowercase letters, digits, hyphens and underscores.
func (g *Graph) validName(n string) bool {
	if n == "" || len(n) > g.opts.MaxNameBytes {
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
func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !g.validName(op.From) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !g.validName(op.From) || !g.validName(op.To) {
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
		if err := g.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
