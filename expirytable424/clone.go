package expirytable424

// cloneLocked deep-copies the table; the caller must hold at least the read lock.
func (t *Table) cloneLocked() *Table {
	c := &Table{
		maxEntries:  t.maxEntries,
		maxKeyBytes: t.maxKeyBytes,
		now:         t.now,
		generation:  t.generation,
		nextRev:     t.nextRev,
		entries:     make(map[string]Entry, len(t.entries)),
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c
}

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cloneLocked(), nil
}
