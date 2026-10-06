package expirytable274

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:         t.opts,
		entries:      entries,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
	}, nil
}
