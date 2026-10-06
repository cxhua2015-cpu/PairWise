package expirytable269

// Clone returns a fully independent deep copy, including logical clocks
// (now, generation, next revision) and options. The clone shares no
// ownership with the source: mutating either table never aliases the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := &Table{
		opts:    t.opts,
		now:     t.now,
		gen:     t.gen,
		nextRev: t.nextRev,
		entries: make(map[string]Entry, len(t.entries)),
	}
	for k, e := range t.entries {
		c.entries[k] = e
	}
	return c, nil
}
