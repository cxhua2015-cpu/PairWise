package topologygraph433

// ValidateBatch performs complete structural validation without reading or
// mutating state. It shares its structural semantics with Apply.
func (g *Graph) ValidateBatch(b Batch) error {
	return g.validate(b)
}

// validate checks structural rules only: known kinds, well-formed names, and
// no extra fields. It never touches graph state.
func (g *Graph) validate(b Batch) error {
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

// validName reports whether s is a non-empty ASCII name of lowercase letters,
// digits, hyphens and underscores within the configured byte limit.
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
