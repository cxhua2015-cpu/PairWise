package topologygraph268

// validateName reports whether name is a non-empty ASCII identifier of at
// most maxBytes bytes, using only [a-z0-9-_].
func validateName(name string, maxBytes int) error {
	if len(name) == 0 || len(name) > maxBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

// validateOp checks the structural shape of a single op: known kind, no
// unexpected fields, and well-formed names in the required positions.
func validateOp(op Op, maxNameBytes int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		return validateName(op.From, maxNameBytes)
	case AddEdge, DeleteEdge:
		if op.From == op.To {
			return ErrInvalidInput
		}
		if err := validateName(op.From, maxNameBytes); err != nil {
			return err
		}
		return validateName(op.To, maxNameBytes)
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
