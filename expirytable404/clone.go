package expirytable404

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable state with the original.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	c := &Table{
		maxEntries:  t.maxEntries,
		maxKeyBytes: t.maxKeyBytes,
		now:         t.now,
		generation:  t.generation,
		nextRev:     t.nextRev,
		entries:     make(map[string]entry, len(t.entries)),
	}
	for k, v := range t.entries {
		c.entries[k] = v
	}
	return c, nil
}
