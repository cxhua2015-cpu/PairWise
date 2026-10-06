package topologygraph288

// validateName reports whether n is a legal node/edge endpoint name:
// non-empty ASCII lowercase letters, digits, hyphens and underscores,
// bounded by the configured byte limit.
func validateName(n string, maxBytes int) bool {
	if n == "" || len(n) > maxBytes {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateStructural performs complete structural validation of a batch
// without reading or mutating graph state. Unknown kinds, malformed names,
// self-loops and unexpected extra fields yield ErrInvalidInput.
func (g *Graph) validateStructural(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case AddNode, DeleteNode:
			if op.To != "" {
				return ErrInvalidInput
			}
			if !validateName(op.From, g.opts.MaxNameBytes) {
				return ErrInvalidInput
			}
		case AddEdge, DeleteEdge:
			if !validateName(op.From, g.opts.MaxNameBytes) ||
				!validateName(op.To, g.opts.MaxNameBytes) {
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

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	return g.validateStructural(b)
}
