package expirytable434

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		opts:         t.opts,
		entries:      make(map[string]Entry, len(t.entries)),
		generation:   t.generation,
		nextRevision: t.nextRevision,
		now:          t.now,
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c, nil
}
