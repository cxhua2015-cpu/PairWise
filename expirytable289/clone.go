package expirytable289

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no slices or maps with the original, so later
// mutations of either table never alias the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		maxEntries:   t.maxEntries,
		maxKeyBytes:  t.maxKeyBytes,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		order:        make([]string, len(t.order)),
		index:        make(map[string]Entry, len(t.index)),
	}
	copy(c.order, t.order)
	for k, e := range t.index {
		c.index[k] = e
	}
	return c, nil
}
