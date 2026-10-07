package expirytable429

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either table
// never affects the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make(map[string]Entry, len(t.state.entries))
	for k, e := range t.state.entries {
		entries[k] = e
	}
	return &Table{
		opts: t.opts,
		state: tableState{
			now:          t.state.now,
			generation:   t.state.generation,
			nextRevision: t.state.nextRevision,
			entries:      entries,
		},
	}, nil
}
