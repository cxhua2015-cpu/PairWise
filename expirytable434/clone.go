package expirytable434

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no mutable memory with the original: entries are
// copied into a fresh map, so later mutations of either table never
// alias the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		opts:         t.opts,
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
