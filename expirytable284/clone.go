package expirytable284

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		maxEntries:   t.maxEntries,
		maxKeyBytes:  t.maxKeyBytes,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		now:          t.now,
		entries:      make(map[string]Entry, len(t.entries)),
		order:        make([]string, len(t.order)),
	}
	for k, v := range t.entries {
		c.entries[k] = v
	}
	copy(c.order, t.order)
	return c, nil
}
