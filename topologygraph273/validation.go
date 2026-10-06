package topologygraph273

// ValidateBatch performs complete structural validation without reading or
// mutating graph state. It shares the exact per-op structural semantics used
// by Apply, so a batch rejected here is rejected there for the same reason.
func (g *Graph) ValidateBatch(b Batch) error {
	for _, op := range b.Ops {
		if err := g.validateOp(op); err != nil {
			return err
		}
	}
	return nil
}
