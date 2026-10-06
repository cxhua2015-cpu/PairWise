package expirytable239

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable memory with the original.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:         t.opts,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		entries:      entries,
	}, nil
}
