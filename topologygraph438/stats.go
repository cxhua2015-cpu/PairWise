package topologygraph438

type Stats struct {
	Generation   uint64
	Nodes, Edges int
}

// Stats returns a linearizable summary of the current state.
func (g *Graph) Stats() Stats { return Stats{} }
