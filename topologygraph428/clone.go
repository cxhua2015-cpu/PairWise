package topologygraph428

// Clone returns a fully independent deep copy, including the logical clock
// (generation). The copy owns freshly allocated node/edge/adjacency maps, so
// later mutations of either graph can never alias the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return &Graph{
		opts:       g.opts,
		generation: g.generation,
		state:      g.state.clone(),
	}, nil
}
