package topologygraph438

// ValidateBatch performs complete structural validation without reading or mutating state.
func (g *Graph) ValidateBatch(b Batch) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return validateStructural(b, g.opts.MaxNameBytes)
}
