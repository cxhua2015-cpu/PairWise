package expirytable264

// Clone returns a fully independent deep copy, including the logical clock,
// generation and revision counter. The clone shares no ownership with the
// original: mutating either table never affects the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		maxEntries:   t.maxEntries,
		maxKeyBytes:  t.maxKeyBytes,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		entries:      entries,
	}, nil
}
