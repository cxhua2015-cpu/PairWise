package topologygraph283

// validateName checks the structural name rules: non-empty, at most
// maxNameBytes bytes, and only ASCII lowercase letters, digits, hyphens
// and underscores.
func validateName(name string, opts Options) error {
	if name == "" || len(name) > opts.MaxNameBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

// validateOp checks the structural rules of a single op without reading
// any graph state: known kind, required fields present, extra fields
// empty, and no self-loops.
func validateOp(op Op, opts Options) error {
	switch op.Kind {
	case AddNode, DeleteNode:
		if op.To != "" {
			return ErrInvalidInput
		}
		return validateName(op.From, opts)
	case AddEdge, DeleteEdge:
		if err := validateName(op.From, opts); err != nil {
			return err
		}
		if err := validateName(op.To, opts); err != nil {
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

// validateBatch performs complete structural validation of a batch
// without reading or mutating graph state.
func validateBatch(b Batch, opts Options) error {
	for _, op := range b.Ops {
		if err := validateOp(op, opts); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBatch performs complete structural validation without reading
// or mutating state. It shares the exact structural semantics of Apply.
func (g *Graph) ValidateBatch(b Batch) error {
	return validateBatch(b, g.opts)
}
