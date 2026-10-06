package expirytable249

// Clone returns a fully independent deep copy, including logical clocks
// (generation, next revision and now). The clone shares no memory with the
// original: subsequent transactions on either table never alias the other.
func (t *Table) Clone() (*Table, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &Table{
		opts:         t.opts,
		entries:      entries,
		generation:   t.generation,
		nextRevision: t.nextRevision,
		now:          t.now,
	}, nil
}
