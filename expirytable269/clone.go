package expirytable269

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	c := &Table{
		maxEntries:  t.maxEntries,
		maxKeyBytes: t.maxKeyBytes,
		now:         t.now,
		gen:         t.gen,
		nextRev:     t.nextRev,
		entries:     make(map[string]Entry, len(t.entries)),
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c, nil
}
