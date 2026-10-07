package topologygraph413

// ValidateBatch performs complete structural validation without reading or
// mutating graph state. Apply shares this exact structural pre-check so both
// entry points agree on what a well-formed batch is.
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

// validName reports whether name is a non-empty ASCII string of lowercase
// letters, digits, hyphens, and underscores within the configured byte limit.
func (g *Graph) validName(name string) bool {
	if name == "" || len(name) > g.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
