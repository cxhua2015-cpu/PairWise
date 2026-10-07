package topologygraph413

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares exactly these structural semantics: any
// batch rejected here is rejected by Apply before any state is read.
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
		// Node ops carry only From; a non-empty To is an extra field.
		if op.To != "" {
			return ErrInvalidInput
		}
		return g.validateName(op.From)
	case AddEdge, DeleteEdge:
		if err := g.validateName(op.From); err != nil {
			return err
		}
		if err := g.validateName(op.To); err != nil {
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

// validateName enforces the name grammar: non-empty, at most MaxNameBytes
// bytes, and only ASCII lowercase letters, digits, '-' and '_'.
func (g *Graph) validateName(n string) error {
	if n == "" || len(n) > g.opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
