package topologygraph413

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no maps with the original: subsequent mutations of
// either graph never alias the other.
func (g *Graph) Clone() (*Graph, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return &Graph{
		opts: g.opts,
		st:   g.st.clone(),
		gen:  g.gen,
	}, nil
}
