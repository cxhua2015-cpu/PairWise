package metacatalog286

// Clone returns a fully independent deep copy, including logical clocks.
// The clone shares no memory with the original, so subsequent mutations of
// either store never alias the other.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		records[k] = cloneRecord(v)
	}
	return &Store{
		opts:         s.opts,
		records:      records,
		totalValue:   s.totalValue,
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}, nil
}

func cloneRecord(r Record) Record {
	return Record{Name: r.Name, Value: append([]byte(nil), r.Value...), Revision: r.Revision}
}
