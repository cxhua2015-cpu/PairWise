package expirytable439

// cloneLocked deep-copies the table; caller must hold t.mu.
func (t *Table) cloneLocked() *Table {
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:         t.opts,
		entries:      entries,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		now:          t.now,
	}
}

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cloneLocked(), nil
}
