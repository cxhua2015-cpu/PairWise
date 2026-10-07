package topologygraph423

// ValidateBatch performs complete structural validation without reading or
// mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	return g.validateBatch(b)
}

// validateBatch checks only structural properties of the batch: known kinds,
// field placement, and name syntax/length. It never touches graph state.
func (g *Graph) validateBatch(b Batch) error {
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

// validName reports whether name is non-empty, fits the byte budget, and
// consists solely of ASCII lowercase letters, digits, hyphens, underscores.
func (g *Graph) validName(name string) bool {
	if name == "" || len(name) > g.opts.MaxNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}
