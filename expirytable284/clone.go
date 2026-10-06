package expirytable284

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no ownership with the original: mutating either table
// never affects the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		maxEntries:  t.maxEntries,
		maxKeyBytes: t.maxKeyBytes,
		now:         t.now,
		generation:  t.generation,
		nextRev:     t.nextRev,
		entries:     entries,
	}, nil
}
