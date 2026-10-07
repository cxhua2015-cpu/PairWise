package expirytable424

// Clone returns a fully independent deep copy. Logical clocks (generation,
// next revision and Now) are preserved, while the mutex, entry map and all
// derived slices have distinct ownership from the receiver.
func (t *Table) Clone() (*Table, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	c := &Table{opt: t.opt, st: t.st.cloneState()}
	return c, nil
}
