package expirytable439

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	c := &Table{
		opts:    t.opts,
		now:     t.now,
		gen:     t.gen,
		nextRev: t.nextRev,
		index:   make(map[string]int, len(t.index)),
		entries: make([]Entry, len(t.entries)),
	}
	for k, v := range t.index {
		c.index[k] = v
	}
	copy(c.entries, t.entries)
	return c, nil
}
