package expirytable224

// Clone returns a fully independent deep copy, including logical clocks.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		opts:    t.opts,
		now:     t.now,
		gen:     t.gen,
		rev:     t.rev,
		entries: make(map[string]Entry, len(t.entries)),
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c, nil
}
