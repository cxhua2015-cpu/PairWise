package expirytable244

// Clone returns a fully independent deep copy, including logical clocks
// (now, generation, nextRevision). The clone shares no slices or maps with
// the original, so later mutations of either table never alias.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	c := &Table{
		maxEntries:   t.maxEntries,
		maxKeyBytes:  t.maxKeyBytes,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		order:        make([]string, len(t.order)),
		items:        make(map[string]Entry, len(t.items)),
	}
	copy(c.order, t.order)
	for k, e := range t.items {
		c.items[k] = e
	}
	return c, nil
}
