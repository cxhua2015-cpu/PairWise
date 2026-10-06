package topologygraph298

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" {
				return ErrInvalidInput
			}
			if err := validName(op.From, g.opts.MaxNameBytes); err != nil {
				return err
			}
		case AddEdge, DeleteEdge:
			if err := validName(op.From, g.opts.MaxNameBytes); err != nil {
				return err
			}
			if err := validName(op.To, g.opts.MaxNameBytes); err != nil {
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

func validName(name string, maxBytes int) error {
	if name == "" || len(name) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}
