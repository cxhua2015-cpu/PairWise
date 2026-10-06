package expirytable234

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either table
// never affects the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		maxEntries:   t.maxEntries,
		maxKeyBytes:  t.maxKeyBytes,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		entries:      make(map[string]Entry, len(t.entries)),
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c, nil
}
