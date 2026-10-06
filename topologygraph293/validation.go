package topologygraph293

// ValidateBatch performs complete structural validation without reading or mutating state.
// It shares the exact structural semantics used by Apply before any state is touched.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
