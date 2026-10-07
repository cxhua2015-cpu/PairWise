package expirytable429

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:    t.opts,
		now:     t.now,
		gen:     t.gen,
		lastRev: t.lastRev,
		entries: entries,
	}, nil
}
