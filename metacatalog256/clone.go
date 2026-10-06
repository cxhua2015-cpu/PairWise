package metacatalog256

import "slices"

// Clone returns a fully independent deep copy, including the logical
// clocks (generation and next revision). Every Value is copied, so the
// clone and the original share no mutable memory and can be mutated
// concurrently without synchronization between them.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clone := &Store{
		opts:         s.opts,
		records:      make(map[string]Record, len(s.records)),
		generation:   s.generation,
		nextRevision: s.nextRevision,
	}
	for name, rec := range s.records {
		clone.records[name] = Record{
			Name:     rec.Name,
			Value:    slices.Clone(rec.Value),
			Revision: rec.Revision,
		}
	}
	return clone, nil
}
