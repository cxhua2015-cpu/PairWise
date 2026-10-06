package topologygraph238

// validName enforces the shared name grammar: non-empty, at most
// MaxNameBytes bytes, ASCII lowercase letters, digits, '-' and '_'.
func (g *Graph) validName(s string) error {
	if len(s) == 0 || len(s) > g.opts.MaxNameBytes {
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. Apply shares exactly these structural semantics.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" {
				return ErrInvalidInput
			}
			if err := g.validName(op.From); err != nil {
				return err
			}
		case AddEdge, DeleteEdge:
			if err := g.validName(op.From); err != nil {
				return err
			}
			if err := g.validName(op.To); err != nil {
				return err
			}
			if op.From == op.To {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}
