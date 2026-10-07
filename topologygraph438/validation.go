package topologygraph438

// ValidateBatch performs complete structural validation without reading or mutating state.
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

func (g *Graph) validateName(s string) error {
	if s == "" || len(s) > g.opts.MaxNameBytes {
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
