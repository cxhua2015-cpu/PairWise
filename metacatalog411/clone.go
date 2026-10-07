package metacatalog411

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
		totalValue:   s.totalValue,
	}
	for name, rec := range s.records {
		out.records[name] = Record{Name: rec.Name, Value: cloneBytes(rec.Value), Revision: rec.Revision}
	}
	return out, nil
}
