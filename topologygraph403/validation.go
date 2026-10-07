package topologygraph403

// validateName enforces the name grammar: non-empty, at most maxBytes long,
// and limited to ASCII lowercase letters, digits, '-' and '_'.
func validateName(s string, maxBytes int) error {
	if len(s) == 0 || len(s) > maxBytes {
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

// validateBatch is the single source of structural truth shared by
// ValidateBatch and Apply. It performs complete structural validation
// without reading or mutating any graph state.
func validateBatch(b Batch, opts Options) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if err := validateName(op.From, opts.MaxNameBytes); err != nil {
				return err
			}
			if op.To != "" {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if err := validateName(op.From, opts.MaxNameBytes); err != nil {
				return err
			}
			if err := validateName(op.To, opts.MaxNameBytes); err != nil {
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

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares the exact structural semantics of Apply.
func (g *Graph) ValidateBatch(b Batch) error {
	g.mu.RLock()
	opts := g.opts
	g.mu.RUnlock()
	return validateBatch(b, opts)
}
