package topologygraph228

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares these exact structural semantics.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) validateOp(op Op) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		if !validName(op.From, g.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
	case AddEdge, DeleteEdge:
		if !validName(op.From, g.opts.MaxNameBytes) || !validName(op.To, g.opts.MaxNameBytes) {
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

// validName reports whether n is a non-empty ASCII name of at most
// maxBytes bytes containing only lowercase letters, digits, '-' and '_'.
func validName(n string, maxBytes int) bool {
	if n == "" || len(n) > maxBytes {
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
