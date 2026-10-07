package topologygraph418

// ValidateBatch performs complete structural validation without reading or
// mutating graph state. It shares the exact structural semantics used by
// Apply: unknown kinds, malformed names, unexpected fields and self-loops
// are rejected with ErrInvalidInput.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
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
	}
	return nil
}

// validName reports whether s is a non-empty ASCII name of lowercase
// letters, digits, hyphens and underscores within the configured byte cap.
func (g *Graph) validName(s string) bool {
	if s == "" || len(s) > g.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
