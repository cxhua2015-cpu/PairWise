package expirytable419

// Clone returns a fully independent deep copy, including logical clocks. The
// clone shares no mutable state with the original: entries are copied into a
// fresh map and each table has its own lock, so subsequent transactions on
// one never affect the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:         t.opts,
		entries:      entries,
		now:          t.now,
		generation:   t.generation,
		nextRevision: t.nextRevision,
	}, nil
}
