package topologygraph418

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original: every map is rebuilt
// so later batches on either graph can never alias the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.candidateLocked(), nil
}
