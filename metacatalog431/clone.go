package metacatalog431

// Clone returns a fully independent deep copy, including logical clocks.
func (s *Store) Clone() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Store{opts: s.opts, state: s.state.cloneState()}, nil
}
