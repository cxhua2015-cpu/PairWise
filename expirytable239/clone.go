package expirytable239

// Clone returns a fully independent deep copy, including the logical
// clocks (now, generation, nextRevision). The clone shares no memory
// with the original: subsequent mutations of either table never alias.
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
