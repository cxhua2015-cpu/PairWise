package topologygraph223

// ValidateBatch performs complete structural validation without reading or
// mutating graph state. Apply shares these exact structural semantics.
func (g *Graph) ValidateBatch(b Batch) error {
	return validateBatch(b, g.maxName)
}

func validateBatch(b Batch, maxName int) error {
	for _, op := range b.Ops {
		if err := validateOp(op, maxName); err != nil {
			return err
		}
	}
	return nil
}

func validateOp(op Op, maxName int) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		return validateName(op.From, maxName)
	case AddEdge, DeleteEdge:
		if err := validateName(op.From, maxName); err != nil {
			return err
		}
		if err := validateName(op.To, maxName); err != nil {
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

// validateName enforces non-empty ASCII [a-z0-9-_] within the byte limit.
func validateName(s string, maxName int) error {
	if s == "" || len(s) > maxName {
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
