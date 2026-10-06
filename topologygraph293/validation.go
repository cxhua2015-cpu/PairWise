package topologygraph293

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	return validateBatch(g.opts, b)
}

// validateBatch checks the structural semantics shared by Apply and
// ValidateBatch. It never reads or mutates graph state.
func validateBatch(opts Options, b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" {
				return ErrInvalidInput
			}
			if !validName(opts, op.From) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validName(opts, op.From) || !validName(opts, op.To) {
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

// validName reports whether name is a non-empty ASCII identifier of
// lowercase letters, digits, hyphens and underscores within the byte limit.
func validName(opts Options, name string) bool {
	if name == "" || len(name) > opts.MaxNameBytes {
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
